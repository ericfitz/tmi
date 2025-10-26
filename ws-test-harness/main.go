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

	"github.com/ericfitz/tmi/internal/slogging"
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

// CollaborationSession matches the OpenAPI CollaborationSession schema
type CollaborationSession struct {
	SessionID       string                   `json:"session_id"`
	Host            string                   `json:"host"`
	Presenter       string                   `json:"presenter"`
	ThreatModelID   string                   `json:"threat_model_id"`
	ThreatModelName string                   `json:"threat_model_name"`
	DiagramID       string                   `json:"diagram_id"`
	DiagramName     string                   `json:"diagram_name"`
	Participants    []CollaborationParticipant `json:"participants"`
	WebSocketURL    string                   `json:"websocket_url"`
}

// CollaborationParticipant matches the OpenAPI Participant schema
type CollaborationParticipant struct {
	User         CollaborationUser `json:"user"`
	LastActivity string            `json:"last_activity"`
	Permissions  string            `json:"permissions"`
}

// CollaborationUser matches the OpenAPI User schema
type CollaborationUser struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

// WebSocketMessage represents the base structure for all AsyncAPI messages
type WebSocketMessage struct {
	MessageType string `json:"message_type"`
}

// User represents user information matching AsyncAPI spec
type User struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

// CurrentPresenterMessage matches AsyncAPI CurrentPresenterPayload
type CurrentPresenterMessage struct {
	MessageType      string `json:"message_type"`
	CurrentPresenter User   `json:"current_presenter"`
}

// ParticipantsUpdateMessage matches AsyncAPI ParticipantsUpdatePayload
type ParticipantsUpdateMessage struct {
	MessageType      string        `json:"message_type"`
	Participants     []Participant `json:"participants"`
	Host             string        `json:"host"`
	CurrentPresenter string        `json:"current_presenter"`
}

type Participant struct {
	User         User   `json:"user"`
	Permissions  string `json:"permissions"`
	LastActivity string `json:"last_activity"`
}

// ParticipantJoinedMessage matches AsyncAPI ParticipantJoinedPayload
type ParticipantJoinedMessage struct {
	MessageType string `json:"message_type"`
	JoinedUser  User   `json:"joined_user"`
	Timestamp   string `json:"timestamp"`
}

// ParticipantLeftMessage matches AsyncAPI ParticipantLeftPayload
type ParticipantLeftMessage struct {
	MessageType   string `json:"message_type"`
	DepartedUser  User   `json:"departed_user"`
	Timestamp     string `json:"timestamp"`
}

// DiagramOperationMessage matches AsyncAPI DiagramOperationPayload
type DiagramOperationMessage struct {
	MessageType    string      `json:"message_type"`
	InitiatingUser User        `json:"initiating_user"`
	OperationID    string      `json:"operation_id"`
	SequenceNumber *uint64     `json:"sequence_number,omitempty"`
	Operation      interface{} `json:"operation"`
}

// ErrorMessage matches AsyncAPI ErrorPayload
type ErrorMessage struct {
	MessageType string `json:"message_type"`
	Error       string `json:"error"`
	Message     string `json:"message"`
	Code        string `json:"code,omitempty"`
	Timestamp   string `json:"timestamp"`
}

// StateCorrectionMessage matches AsyncAPI StateCorrectionPayload
type StateCorrectionMessage struct {
	MessageType  string `json:"message_type"`
	UpdateVector *int64 `json:"update_vector"`
}

func main() {
	config := parseArgs()

	slogging.Get().GetSlogger().Info("WebSocket Test Harness starting")
	slogging.Get().GetSlogger().Info("Configuration", "server", config.ServerURL, "user_hint", config.UserHint, "mode", func() string {
		if config.IsHost {
			return "Host"
		}
		return "Participant"
	}())
	if config.IsHost && len(config.Participants) > 0 {
		slogging.Get().GetSlogger().Info("Host mode participants", "participants", strings.Join(config.Participants, ", "))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		slogging.Get().GetSlogger().Info("Shutting down")
		cancel()
	}()

	// Perform OAuth login
	tokens, err := performOAuthLogin(ctx, config)
	if err != nil {
		slogging.Get().GetSlogger().Error("OAuth login failed", "error", err)
		os.Exit(1)
	}

	slogging.Get().GetSlogger().Info("Login successful", "access_token_prefix", tokens.AccessToken[:20])

	if config.IsHost {
		err = runHostMode(ctx, config, tokens)
	} else {
		err = runParticipantMode(ctx, config, tokens)
	}

	if err != nil {
		slogging.Get().GetSlogger().Error("Application error", "error", err)
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
		slogging.Get().GetSlogger().Error("Required parameter missing", "parameter", "user")
		os.Exit(1)
	}

	if participantsStr != "" && !isHost {
		slogging.Get().GetSlogger().Error("Invalid parameter combination", "error", "participants can only be used with host mode")
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

	slogging.Get().GetSlogger().Debug("OAuth authorization request", "url", authURL.String())

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

	slogging.Get().GetSlogger().Debug("OAuth authorization response", "status_code", resp.StatusCode, "status", resp.Status)

	// OAuth should redirect to our callback
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
		// Get the redirect location
		redirectURL := resp.Header.Get("Location")
		slogging.Get().GetSlogger().Debug("OAuth redirect", "location", redirectURL)

		// The test OAuth provider might redirect directly with tokens in the fragment
		// We need to follow this redirect ourselves
		if redirectURL != "" {
			redirectResp, err := http.Get(redirectURL)
			if err != nil {
				slogging.Get().GetSlogger().Warn("Error following OAuth redirect", "error", err)
			} else {
				redirectResp.Body.Close()
			}
		}
	} else {
		body, _ := io.ReadAll(resp.Body)
		slogging.Get().GetSlogger().Debug("OAuth unexpected response", "body", string(body))
		return nil, fmt.Errorf("Expected redirect, got status %d", resp.StatusCode)
	}

	// Wait for callback
	slogging.Get().GetSlogger().Info("Waiting for OAuth callback")
	select {
	case tokens := <-callbackHandler.tokens:
		slogging.Get().GetSlogger().Info("Received tokens from OAuth callback")
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
		slogging.Get().GetSlogger().Debug("OAuth callback received", "method", r.Method, "url", r.URL.String())

		// Log all query parameters
		for k, v := range r.URL.Query() {
			slogging.Get().GetSlogger().Debug("OAuth callback parameter", "key", k, "value", strings.Join(v, ", "))
		}

		// Check for implicit flow tokens
		accessToken := r.URL.Query().Get("access_token")
		if accessToken != "" {
			// Implicit flow
			slogging.Get().GetSlogger().Debug("Detected implicit flow OAuth response")
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
			slogging.Get().GetSlogger().Debug("Detected authorization code flow OAuth response")
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
	slogging.Get().GetSlogger().Info("Running in Host Mode")

	// Create threat model with participants
	threatModel, err := createThreatModel(config, tokens, config.Participants)
	if err != nil {
		return fmt.Errorf("failed to create threat model: %w", err)
	}
	slogging.Get().GetSlogger().Info("Created threat model", "name", threatModel.Name, "id", threatModel.ID)

	// Create diagram
	diagram, err := createDiagram(config, tokens, threatModel.ID)
	if err != nil {
		return fmt.Errorf("failed to create diagram: %w", err)
	}
	slogging.Get().GetSlogger().Info("Created diagram", "name", diagram.Name, "id", diagram.ID)

	// Start collaboration session
	session, err := startCollaborationSession(config, tokens, threatModel.ID, diagram.ID)
	if err != nil {
		return fmt.Errorf("failed to start collaboration session: %w", err)
	}
	slogging.Get().GetSlogger().Info("Started collaboration session", "session_id", session.SessionID)

	// Connect to WebSocket
	return connectToWebSocket(ctx, config, tokens, threatModel.ID, diagram.ID)
}

func runParticipantMode(ctx context.Context, config Config, tokens *AuthTokens) error {
	slogging.Get().GetSlogger().Info("Running in Participant Mode")
	slogging.Get().GetSlogger().Info("Polling for available collaboration sessions")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			session, threatModelID, diagramID, err := findAvailableSession(config, tokens)
			if err != nil {
				slogging.Get().GetSlogger().Warn("Error checking for sessions", "error", err)
				continue
			}
			if session != nil {
				slogging.Get().GetSlogger().Info("Found collaboration session", "session_id", session.SessionID, "host", session.Host)

				// Connect to WebSocket - if it disconnects, we'll return here and continue polling
				err = connectToWebSocket(ctx, config, tokens, threatModelID, diagramID)
				if err != nil {
					slogging.Get().GetSlogger().Info("WebSocket connection ended", "error", err)
					slogging.Get().GetSlogger().Info("Waiting 3 seconds before returning to polling")

					// Wait a bit before trying again to avoid hammering the server
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(3 * time.Second):
						slogging.Get().GetSlogger().Info("Returning to polling for collaboration sessions")
						// Continue the loop to start polling again
						continue
					}
				}

				// If connectToWebSocket returns without error, it means context was cancelled
				return nil
			}
			slogging.Get().GetSlogger().Debug("No available sessions found, continuing to poll")
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
		slogging.Get().GetSlogger().Debug("Adding participant", "email", email, "permission", perm)
	}

	payload := map[string]interface{}{
		"name":          fmt.Sprintf("Test TM - %s - %s", config.UserHint, time.Now().Format("15:04:05")),
		"description":   "Test threat model created by WebSocket test harness",
		"authorization": authorization,
	}

	body, _ := json.Marshal(payload)
	slogging.Get().GetSlogger().Debug("CreateThreatModel API request", "method", "POST", "url", url, "body", string(body))

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
	slogging.Get().GetSlogger().Debug("CreateThreatModel API response", "status_code", resp.StatusCode, "status", resp.Status)
	if resp.StatusCode != http.StatusCreated {
		slogging.Get().GetSlogger().Debug("CreateThreatModel API error response", "body", string(respBody))
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
	slogging.Get().GetSlogger().Debug("CreateDiagram API request", "method", "POST", "url", url, "body", string(body))

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
	slogging.Get().GetSlogger().Debug("CreateDiagram API response", "status_code", resp.StatusCode, "status", resp.Status)
	if resp.StatusCode != http.StatusCreated {
		slogging.Get().GetSlogger().Debug("CreateDiagram API error response", "body", string(respBody))
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

	slogging.Get().GetSlogger().Debug("StartCollaborationSession API request", "method", "POST", "url", url)

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
	slogging.Get().GetSlogger().Debug("StartCollaborationSession API response", "status_code", resp.StatusCode, "status", resp.Status)

	// Per OpenAPI spec, only 201 indicates successful creation
	if resp.StatusCode != http.StatusCreated {
		slogging.Get().GetSlogger().Debug("StartCollaborationSession API error response", "body", string(respBody))
		return nil, fmt.Errorf("failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var session CollaborationSession
	if err := json.Unmarshal(respBody, &session); err != nil {
		return nil, fmt.Errorf("failed to parse collaboration session response: %w", err)
	}

	slogging.Get().GetSlogger().Info("Collaboration session created",
		"session_id", session.SessionID,
		"host", session.Host,
		"presenter", session.Presenter,
		"websocket_url", session.WebSocketURL)

	return &session, nil
}

func findAvailableSession(config Config, tokens *AuthTokens) (*CollaborationSession, string, string, error) {
	// Get list of active collaboration sessions
	url := fmt.Sprintf("%s/collaboration/sessions", config.ServerURL)
	slogging.Get().GetSlogger().Debug("FindAvailableSession API request", "method", "GET", "url", url)

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
	slogging.Get().GetSlogger().Debug("FindAvailableSession API response", "status_code", resp.StatusCode, "status", resp.Status)
	if resp.StatusCode != http.StatusOK {
		slogging.Get().GetSlogger().Debug("FindAvailableSession API error response", "body", string(respBody))
		return nil, "", "", fmt.Errorf("failed with status %d", resp.StatusCode)
	}

	// Parse the response as an array of collaboration sessions
	var sessions []CollaborationSession
	if err := json.Unmarshal(respBody, &sessions); err != nil {
		return nil, "", "", fmt.Errorf("failed to parse sessions response: %w", err)
	}

	// If there are any sessions, return the first one
	if len(sessions) > 0 {
		session := sessions[0]
		slogging.Get().GetSlogger().Debug("Found active sessions", "count", len(sessions))
		slogging.Get().GetSlogger().Info("Selected session details",
			"session_id", session.SessionID,
			"host", session.Host,
			"presenter", session.Presenter,
			"threat_model", session.ThreatModelName,
			"diagram", session.DiagramName,
			"participants", len(session.Participants))

		return &session, session.ThreatModelID, session.DiagramID, nil
	}

	return nil, "", "", nil
}

func connectToWebSocket(ctx context.Context, config Config, tokens *AuthTokens, threatModelID, diagramID string) error {
	// Build WebSocket URL
	wsURL := strings.Replace(config.ServerURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = fmt.Sprintf("%s/threat_models/%s/diagrams/%s/ws?token=%s",
		wsURL, threatModelID, diagramID, tokens.AccessToken)

	slogging.Get().GetSlogger().Info("Connecting to WebSocket", "url", wsURL)

	// Connect to WebSocket
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			slogging.Get().GetSlogger().Error("WebSocket connection failed", "status_code", resp.StatusCode, "status", resp.Status, "body", string(body))
		}
		return fmt.Errorf("WebSocket connection failed: %w", err)
	}
	defer conn.Close()

	slogging.Get().GetSlogger().Info("WebSocket connected successfully")

	// Channel to signal when connection is lost
	connectionLost := make(chan error, 1)

	// Start message reader
	go func() {
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				slogging.Get().GetSlogger().Warn("WebSocket read error", "error", err)
				connectionLost <- err
				return
			}

			timestamp := time.Now().Format("15:04:05.000")
			slogging.Get().GetSlogger().Debug("Received WebSocket message", "type", messageType, "timestamp", timestamp)

			// Parse the message to determine its type
			var baseMsg WebSocketMessage
			if err := json.Unmarshal(message, &baseMsg); err != nil {
				slogging.Get().GetSlogger().Warn("Failed to parse message type", "error", err, "raw_message", string(message))
				continue
			}

			// Handle different message types according to AsyncAPI spec
			switch baseMsg.MessageType {
			case "current_presenter":
				var msg CurrentPresenterMessage
				if err := json.Unmarshal(message, &msg); err == nil {
					slogging.Get().GetSlogger().Info("Current Presenter",
						"user_id", msg.CurrentPresenter.UserID,
						"email", msg.CurrentPresenter.Email,
						"display_name", msg.CurrentPresenter.DisplayName)
				}

			case "participants_update":
				var msg ParticipantsUpdateMessage
				if err := json.Unmarshal(message, &msg); err == nil {
					slogging.Get().GetSlogger().Info("Participants Update",
						"participant_count", len(msg.Participants),
						"host", msg.Host,
						"current_presenter", msg.CurrentPresenter)
					for i, p := range msg.Participants {
						slogging.Get().GetSlogger().Debug("Participant",
							"index", i,
							"user_id", p.User.UserID,
							"email", p.User.Email,
							"permissions", p.Permissions,
							"last_activity", p.LastActivity)
					}
				}

			case "participant_joined":
				var msg ParticipantJoinedMessage
				if err := json.Unmarshal(message, &msg); err == nil {
					slogging.Get().GetSlogger().Info("Participant Joined",
						"user_id", msg.JoinedUser.UserID,
						"email", msg.JoinedUser.Email,
						"display_name", msg.JoinedUser.DisplayName,
						"timestamp", msg.Timestamp)
				}

			case "participant_left":
				var msg ParticipantLeftMessage
				if err := json.Unmarshal(message, &msg); err == nil {
					slogging.Get().GetSlogger().Info("Participant Left",
						"user_id", msg.DepartedUser.UserID,
						"email", msg.DepartedUser.Email,
						"display_name", msg.DepartedUser.DisplayName,
						"timestamp", msg.Timestamp)
				}

			case "diagram_operation":
				var msg DiagramOperationMessage
				if err := json.Unmarshal(message, &msg); err == nil {
					slogging.Get().GetSlogger().Info("Diagram Operation",
						"operation_id", msg.OperationID,
						"initiating_user", msg.InitiatingUser.Email,
						"sequence_number", msg.SequenceNumber)
				}

			case "state_correction":
				var msg StateCorrectionMessage
				if err := json.Unmarshal(message, &msg); err == nil {
					slogging.Get().GetSlogger().Info("State Correction",
						"update_vector", msg.UpdateVector)
				}

			case "error":
				var msg ErrorMessage
				if err := json.Unmarshal(message, &msg); err == nil {
					slogging.Get().GetSlogger().Error("WebSocket Error Message",
						"error", msg.Error,
						"message", msg.Message,
						"code", msg.Code,
						"timestamp", msg.Timestamp)
				}

			default:
				// For unknown message types, pretty-print the entire JSON
				slogging.Get().GetSlogger().Debug("Unknown message type", "message_type", baseMsg.MessageType)
			}

			// Always log the full JSON for debugging purposes
			var prettyJSON bytes.Buffer
			if err := json.Indent(&prettyJSON, message, "", "  "); err == nil {
				slogging.Get().GetSlogger().Debug("Full message JSON", "json", prettyJSON.String())
			} else {
				slogging.Get().GetSlogger().Debug("Full message JSON (raw)", "message", string(message))
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
			slogging.Get().GetSlogger().Warn("Error sending WebSocket close message", "error", err)
		}
		return nil
	case err := <-connectionLost:
		// Connection was lost - return error so caller can handle reconnection
		return fmt.Errorf("WebSocket connection lost: %w", err)
	}
}
