package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	ServerURL    string
	UserHint     string
	IsHost       bool
	Participants []string
}

type AuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    string `json:"expires_in"`
	State        string `json:"state"`
}

type OAuthCallbackHandler struct {
	tokens    chan AuthTokens
	errorChan chan error
	port      int
}

type ThreatModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Diagram struct {
	ID            string `json:"id"`
	ThreatModelID string `json:"threat_model_id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
}

type CollaborationSession struct {
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	LastActivity   time.Time `json:"last_activity"`
	DiagramVersion int       `json:"diagram_version"`
	ActiveClients  int       `json:"active_clients"`
	HostID         string    `json:"host_id"`
	HostEmail      string    `json:"host_email"`
}

type WebSocketMessage struct {
	MessageType string      `json:"message_type"`
	User        *User       `json:"user,omitempty"`
	OperationID string      `json:"operation_id,omitempty"`
	Operation   interface{} `json:"operation,omitempty"`
	Timestamp   string      `json:"timestamp,omitempty"`
}

type User struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

func main() {
	config := parseArgs()

	fmt.Printf("WebSocket Test Harness starting...\n")
	fmt.Printf("Server: %s\n", config.ServerURL)
	fmt.Printf("User hint: %s\n", config.UserHint)
	fmt.Printf("Mode: %s\n", func() string {
		if config.IsHost {
			return "Host"
		}
		return "Participant"
	}())
	if config.IsHost && len(config.Participants) > 0 {
		fmt.Printf("Participants: %s\n", strings.Join(config.Participants, ", "))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Perform OAuth login
	tokens, err := performOAuthLogin(ctx, config)
	if err != nil {
		fmt.Printf("OAuth login failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Login successful! Access token: %s...\n", tokens.AccessToken[:20])

	if config.IsHost {
		err = runHostMode(ctx, config, tokens)
	} else {
		err = runParticipantMode(ctx, config, tokens)
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func parseArgs() Config {
	var serverURL, userHint, participantsStr string
	var isHost bool

	flag.StringVar(&serverURL, "server", "localhost:8080", "Server URL")
	flag.StringVar(&userHint, "user", "", "User hint for login")
	flag.BoolVar(&isHost, "host", false, "Run as host mode")
	flag.StringVar(&participantsStr, "participants", "", "Comma-separated list of participant user hints (host mode only)")
	flag.Parse()

	if userHint == "" {
		fmt.Fprintf(os.Stderr, "Error: --user is required\n")
		os.Exit(1)
	}

	if participantsStr != "" && !isHost {
		fmt.Fprintf(os.Stderr, "Error: --participants can only be used with --host\n")
		os.Exit(1)
	}

	// Ensure server URL has protocol
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "http://" + serverURL
	}

	config := Config{
		ServerURL: serverURL,
		UserHint:  userHint,
		IsHost:    isHost,
	}

	if participantsStr != "" {
		config.Participants = strings.Split(participantsStr, ",")
		for i, p := range config.Participants {
			config.Participants[i] = strings.TrimSpace(p)
		}
	}

	return config
}

func performOAuthLogin(ctx context.Context, config Config) (*AuthTokens, error) {
	// Start local callback handler
	callbackHandler := &OAuthCallbackHandler{
		tokens:    make(chan AuthTokens),
		errorChan: make(chan error),
	}

	listener, err := startCallbackServer(ctx, callbackHandler)
	if err != nil {
		return nil, fmt.Errorf("failed to start callback server: %w", err)
	}
	defer listener.Close()

	// Get the callback URL
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	callbackURL := fmt.Sprintf("http://localhost:%s/callback", port)

	// Build OAuth authorization URL
	authURL, err := url.Parse(config.ServerURL + "/oauth2/authorize")
	if err != nil {
		return nil, fmt.Errorf("failed to parse auth URL: %w", err)
	}

	query := authURL.Query()
	query.Set("idp", "test")
	query.Set("login_hint", config.UserHint)
	query.Set("client_callback", callbackURL)
	query.Set("scope", "openid email profile")
	authURL.RawQuery = query.Encode()

	fmt.Printf("Attempting: GET %s\n", authURL.String())

	// Make the OAuth authorization request
	// Don't follow redirects automatically since we need the callback to go to our server
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(authURL.String())
	if err != nil {
		return nil, fmt.Errorf("OAuth authorization request failed: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("OAuth authorization response: %d %s\n", resp.StatusCode, resp.Status)

	// OAuth should redirect to our callback
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
		// Get the redirect location
		redirectURL := resp.Header.Get("Location")
		fmt.Printf("Redirect location: %s\n", redirectURL)

		// The test OAuth provider might redirect directly with tokens in the fragment
		// We need to follow this redirect ourselves
		if redirectURL != "" {
			redirectResp, err := http.Get(redirectURL)
			if err != nil {
				fmt.Printf("Error following redirect: %v\n", err)
			} else {
				redirectResp.Body.Close()
			}
		}
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Response body: %s\n", string(body))
		return nil, fmt.Errorf("Expected redirect, got status %d", resp.StatusCode)
	}

	// Wait for callback
	fmt.Println("Waiting for OAuth callback...")
	select {
	case tokens := <-callbackHandler.tokens:
		fmt.Println("Received tokens from callback")
		return &tokens, nil
	case err := <-callbackHandler.errorChan:
		return nil, fmt.Errorf("OAuth callback error: %w", err)
	case <-ctx.Done():
		return nil, fmt.Errorf("OAuth login cancelled")
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("OAuth callback timeout")
	}
}

func startCallbackServer(ctx context.Context, handler *OAuthCallbackHandler) (net.Listener, error) {
	// Start on a random port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("OAuth callback received: %s %s\n", r.Method, r.URL.String())

		// Log all query parameters
		for k, v := range r.URL.Query() {
			fmt.Printf("  Query param: %s = %s\n", k, strings.Join(v, ", "))
		}

		// Check for implicit flow tokens
		accessToken := r.URL.Query().Get("access_token")
		if accessToken != "" {
			// Implicit flow
			fmt.Println("Detected implicit flow OAuth response")
			tokens := AuthTokens{
				AccessToken:  accessToken,
				RefreshToken: r.URL.Query().Get("refresh_token"),
				TokenType:    r.URL.Query().Get("token_type"),
				ExpiresIn:    r.URL.Query().Get("expires_in"),
				State:        r.URL.Query().Get("state"),
			}

			// Send tokens to the channel in a goroutine to avoid blocking the HTTP handler
			go func() {
				handler.tokens <- tokens
			}()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OAuth callback received successfully. You can close this window."))
			return
		}

		// Check for authorization code flow
		code := r.URL.Query().Get("code")
		if code != "" {
			// Authorization code flow - would need to exchange code for tokens
			fmt.Println("Detected authorization code flow OAuth response")
			// For now, we'll error as the test provider uses implicit flow
			handler.errorChan <- fmt.Errorf("authorization code flow not implemented (received code: %s)", code)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Authorization code flow not supported"))
			return
		}

		// Error case
		errorMsg := r.URL.Query().Get("error")
		if errorMsg != "" {
			errorDesc := r.URL.Query().Get("error_description")
			handler.errorChan <- fmt.Errorf("OAuth error: %s - %s", errorMsg, errorDesc)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(fmt.Sprintf("OAuth error: %s", html.EscapeString(errorMsg))))
			return
		}

		// Unknown response
		handler.errorChan <- fmt.Errorf("unknown OAuth callback format")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Unknown OAuth callback format"))
	})

	server := &http.Server{Handler: mux}
	go func() {
		server.Serve(listener)
	}()

	// Shutdown server when context is cancelled
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return listener, nil
}

func runHostMode(ctx context.Context, config Config, tokens *AuthTokens) error {
	fmt.Println("\n=== Running in Host Mode ===")

	// Create threat model with participants
	threatModel, err := createThreatModel(config, tokens, config.Participants)
	if err != nil {
		return fmt.Errorf("failed to create threat model: %w", err)
	}
	fmt.Printf("Created threat model: %s (ID: %s)\n", threatModel.Name, threatModel.ID)

	// Create diagram
	diagram, err := createDiagram(config, tokens, threatModel.ID)
	if err != nil {
		return fmt.Errorf("failed to create diagram: %w", err)
	}
	fmt.Printf("Created diagram: %s (ID: %s)\n", diagram.Name, diagram.ID)

	// Start collaboration session
	session, err := startCollaborationSession(config, tokens, threatModel.ID, diagram.ID)
	if err != nil {
		return fmt.Errorf("failed to start collaboration session: %w", err)
	}
	fmt.Printf("Started collaboration session: %s\n", session.ID)

	// Connect to WebSocket
	return connectToWebSocket(ctx, config, tokens, threatModel.ID, diagram.ID)
}

func runParticipantMode(ctx context.Context, config Config, tokens *AuthTokens) error {
	fmt.Println("\n=== Running in Participant Mode ===")
	fmt.Println("Polling for available collaboration sessions...")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			session, threatModelID, diagramID, err := findAvailableSession(config, tokens)
			if err != nil {
				fmt.Printf("Error checking for sessions: %v\n", err)
				continue
			}
			if session != nil {
				fmt.Printf("Found collaboration session: %s (Host: %s)\n", session.ID, session.HostEmail)

				// Connect to WebSocket - if it disconnects, we'll return here and continue polling
				err = connectToWebSocket(ctx, config, tokens, threatModelID, diagramID)
				if err != nil {
					fmt.Printf("WebSocket connection ended: %v\n", err)
					fmt.Println("Waiting 3 seconds before returning to polling...")

					// Wait a bit before trying again to avoid hammering the server
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(3 * time.Second):
						fmt.Println("Returning to polling for collaboration sessions...")
						// Continue the loop to start polling again
						continue
					}
				}

				// If connectToWebSocket returns without error, it means context was cancelled
				return nil
			}
			fmt.Println("No available sessions found, continuing to poll...")
		}
	}
}

func createThreatModel(config Config, tokens *AuthTokens, participants []string) (*ThreatModel, error) {
	url := fmt.Sprintf("%s/threat_models", config.ServerURL)

	// Build authorization array with participants
	authorization := []map[string]string{}

	// Add participants if specified
	for _, participant := range participants {
		// Convert hint to email format if needed
		email := participant
		if !strings.Contains(email, "@") {
			email = fmt.Sprintf("%s@test.tmi", participant)
		}

		// Randomly select permission
		permissions := []string{"reader", "writer", "owner"}
		perm := permissions[rand.Intn(len(permissions))]

		authorization = append(authorization, map[string]string{
			"subject": email,
			"role":    perm,
		})
		fmt.Printf("Adding participant %s with %s permission\n", email, perm)
	}

	payload := map[string]interface{}{
		"name":          fmt.Sprintf("Test TM - %s - %s", config.UserHint, time.Now().Format("15:04:05")),
		"description":   "Test threat model created by WebSocket test harness",
		"authorization": authorization,
	}

	body, _ := json.Marshal(payload)
	fmt.Printf("Attempting: POST %s\n", url)
	fmt.Printf("Request body: %s\n", string(body))

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokens.AccessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response: %d %s\n", resp.StatusCode, resp.Status)
	if resp.StatusCode != http.StatusCreated {
		fmt.Printf("Response body: %s\n", string(respBody))
		return nil, fmt.Errorf("failed with status %d", resp.StatusCode)
	}

	var threatModel ThreatModel
	if err := json.Unmarshal(respBody, &threatModel); err != nil {
		return nil, err
	}

	return &threatModel, nil
}

func createDiagram(config Config, tokens *AuthTokens, threatModelID string) (*Diagram, error) {
	url := fmt.Sprintf("%s/threat_models/%s/diagrams", config.ServerURL, threatModelID)

	payload := map[string]interface{}{
		"name": fmt.Sprintf("Test Diagram - %s", time.Now().Format("15:04:05")),
		"type": "DFD-1.0.0",
	}

	body, _ := json.Marshal(payload)
	fmt.Printf("Attempting: POST %s\n", url)
	fmt.Printf("Request body: %s\n", string(body))

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokens.AccessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response: %d %s\n", resp.StatusCode, resp.Status)
	if resp.StatusCode != http.StatusCreated {
		fmt.Printf("Response body: %s\n", string(respBody))
		return nil, fmt.Errorf("failed with status %d", resp.StatusCode)
	}

	var diagram Diagram
	if err := json.Unmarshal(respBody, &diagram); err != nil {
		return nil, err
	}

	return &diagram, nil
}

func startCollaborationSession(config Config, tokens *AuthTokens, threatModelID, diagramID string) (*CollaborationSession, error) {
	url := fmt.Sprintf("%s/threat_models/%s/diagrams/%s/collaborate", config.ServerURL, threatModelID, diagramID)

	fmt.Printf("Attempting: POST %s\n", url)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokens.AccessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response: %d %s\n", resp.StatusCode, resp.Status)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		fmt.Printf("Response body: %s\n", string(respBody))
		return nil, fmt.Errorf("failed with status %d", resp.StatusCode)
	}

	var session CollaborationSession
	if err := json.Unmarshal(respBody, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func findAvailableSession(config Config, tokens *AuthTokens) (*CollaborationSession, string, string, error) {
	// Get list of active collaboration sessions
	url := fmt.Sprintf("%s/collaboration/sessions", config.ServerURL)
	fmt.Printf("Attempting: GET %s\n", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", "", err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokens.AccessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response: %d %s\n", resp.StatusCode, resp.Status)
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Response body: %s\n", string(respBody))
		return nil, "", "", fmt.Errorf("failed with status %d", resp.StatusCode)
	}

	// Parse the response as an array of collaboration sessions
	var sessions []struct {
		SessionID       string `json:"session_id"`
		Host            string `json:"host"`
		Presenter       string `json:"presenter"`
		ThreatModelID   string `json:"threat_model_id"`
		ThreatModelName string `json:"threat_model_name"`
		DiagramID       string `json:"diagram_id"`
		DiagramName     string `json:"diagram_name"`
		WebSocketURL    string `json:"websocket_url"`
		Participants    []struct {
			UserID      string `json:"user_id"`
			Email       string `json:"email"`
			DisplayName string `json:"displayName"`
			Role        string `json:"role"`
			IsHost      bool   `json:"is_host"`
			IsPresenter bool   `json:"is_presenter"`
		} `json:"participants"`
	}
	if err := json.Unmarshal(respBody, &sessions); err != nil {
		return nil, "", "", fmt.Errorf("failed to parse sessions response: %w", err)
	}

	// If there are any sessions, return the first one
	if len(sessions) > 0 {
		session := sessions[0]
		fmt.Printf("Found %d active session(s)\n", len(sessions))

		// Convert to CollaborationSession format (matching the existing structure)
		collabSession := &CollaborationSession{
			ID:            session.SessionID,
			HostID:        session.Host,
			HostEmail:     session.Host,
			ActiveClients: len(session.Participants),
		}

		return collabSession, session.ThreatModelID, session.DiagramID, nil
	}

	return nil, "", "", nil
}

func connectToWebSocket(ctx context.Context, config Config, tokens *AuthTokens, threatModelID, diagramID string) error {
	// Build WebSocket URL
	wsURL := strings.Replace(config.ServerURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = fmt.Sprintf("%s/threat_models/%s/diagrams/%s/ws?token=%s",
		wsURL, threatModelID, diagramID, tokens.AccessToken)

	fmt.Printf("\nConnecting to WebSocket: %s\n", wsURL)

	// Connect to WebSocket
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("WebSocket connection failed. Response: %d %s\n", resp.StatusCode, resp.Status)
			fmt.Printf("Response body: %s\n", string(body))
		}
		return fmt.Errorf("WebSocket connection failed: %w", err)
	}
	defer conn.Close()

	fmt.Println("WebSocket connected successfully!")

	// Channel to signal when connection is lost
	connectionLost := make(chan error, 1)

	// Start message reader
	go func() {
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("WebSocket read error: %v\n", err)
				connectionLost <- err
				return
			}

			fmt.Printf("\n[%s] Received WebSocket message (type: %d):\n",
				time.Now().Format("15:04:05.000"), messageType)

			// Try to pretty-print JSON
			var prettyJSON bytes.Buffer
			if err := json.Indent(&prettyJSON, message, "", "  "); err == nil {
				fmt.Println(prettyJSON.String())
			} else {
				fmt.Printf("Raw message: %s\n", string(message))
			}
		}
	}()

	// Wait for either context cancellation or connection loss
	select {
	case <-ctx.Done():
		// Context cancelled - clean shutdown
		err = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			fmt.Printf("Error sending close message: %v\n", err)
		}
		return nil
	case err := <-connectionLost:
		// Connection was lost - return error so caller can handle reconnection
		return fmt.Errorf("WebSocket connection lost: %w", err)
	}
}
