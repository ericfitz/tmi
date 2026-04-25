//go:build manual

package manual

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestGoogleWorkspaceDelegatedFlow is a developer-driven manual test.
//
// Prerequisites:
//  1. TMI server running locally with google_workspace enabled
//     (see config-development.yml + .env.dev for content_oauth and
//     content_sources.google_workspace).
//  2. TMI_CONTENT_TOKEN_ENCRYPTION_KEY set on the running server.
//  3. The OAuth callback stub running (`make start-oauth-stub`).
//  4. Tester has a real Google account with at least one Google Doc.
//  5. Env vars set:
//     TMI_BASE_URL                 (defaults to http://localhost:8080)
//     TMI_MANUAL_JWT               bearer token for the tester's user
//     TMI_MANUAL_THREAT_MODEL_ID   uuid of a threat model owned by the tester
//
// Run with: make test-manual-google-workspace
func TestGoogleWorkspaceDelegatedFlow(t *testing.T) {
	baseURL := os.Getenv("TMI_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	jwt := os.Getenv("TMI_MANUAL_JWT")
	if jwt == "" {
		t.Fatalf("set TMI_MANUAL_JWT to a bearer token for your user (use /oauth/init + flow)")
	}

	reader := bufio.NewReader(os.Stdin)

	// Step 1: authorize delegated account.
	fmt.Println("Authorizing google_workspace for your user...")
	authURL := authorizeContentToken(t, baseURL, jwt)
	fmt.Printf("Open this URL in a browser and consent:\n  %s\n", authURL)
	fmt.Println("When done, press Enter to continue.")
	_, _ = reader.ReadString('\n')

	// Step 2: mint picker token.
	pickerResp := mintPickerToken(t, baseURL, jwt)
	fmt.Printf("Picker token minted; expires at %s\n", pickerResp["expires_at"])

	// Step 3: serve picker harness.
	harnessURL := servePickerHarness(t, pickerResp)
	fmt.Printf("Open the picker harness in a browser and pick a file:\n  %s\n", harnessURL)
	fmt.Print("Paste the picked file's JSON (one object from the docs array) here: ")
	raw, _ := reader.ReadString('\n')
	var picked map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &picked); err != nil {
		t.Fatalf("bad picker JSON: %v", err)
	}

	// Step 4: attach document with picker_registration.
	docID := attachDocument(t, baseURL, jwt, picked)
	fmt.Printf("Document created: %s\n", docID)

	// Step 5: trigger fetch + verify access_status == accessible.
	status := pollAccessStatus(t, baseURL, jwt, docID, 30*time.Second)
	if status != "accessible" {
		t.Fatalf("expected access_status=accessible, got %s", status)
	}

	// Step 6: cleanup.
	deleteContentToken(t, baseURL, jwt)
	fmt.Println("Test complete.")
}

func httpJSON(t *testing.T, method, url, jwt string, body interface{}) map[string]interface{} {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reqBody = bytes.NewReader(raw)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http %s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("http %s %s: %d %s", method, url, resp.StatusCode, string(raw))
	}
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil && err != io.EOF {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func authorizeContentToken(t *testing.T, baseURL, jwt string) string {
	body := map[string]string{"client_callback": "http://localhost:8079/"}
	resp := httpJSON(t, "POST", baseURL+"/me/content_tokens/google_workspace/authorize", jwt, body)
	return resp["authorization_url"].(string)
}

func mintPickerToken(t *testing.T, baseURL, jwt string) map[string]string {
	resp := httpJSON(t, "POST", baseURL+"/me/picker_tokens/google_workspace", jwt, nil)
	return map[string]string{
		"access_token":  resp["access_token"].(string),
		"developer_key": resp["developer_key"].(string),
		"app_id":        resp["app_id"].(string),
		"expires_at":    resp["expires_at"].(string),
	}
}

func servePickerHarness(t *testing.T, pickerData map[string]string) string {
	t.Helper()
	srv := &http.Server{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("../../../scripts/google-picker-harness")))
	srv.Handler = mux
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	u := fmt.Sprintf("http://%s/index.html?access_token=%s&developer_key=%s&app_id=%s",
		ln.Addr().String(),
		pickerData["access_token"], pickerData["developer_key"], pickerData["app_id"])
	return u
}

func attachDocument(t *testing.T, baseURL, jwt string, picked map[string]interface{}) string {
	tmID := os.Getenv("TMI_MANUAL_THREAT_MODEL_ID")
	if tmID == "" {
		t.Fatalf("set TMI_MANUAL_THREAT_MODEL_ID to a threat model owned by your user")
	}
	fileID, _ := picked["id"].(string)
	uri, _ := picked["url"].(string)
	mime, _ := picked["mimeType"].(string)
	name, _ := picked["name"].(string)
	body := map[string]interface{}{
		"name": name,
		"uri":  uri,
		"picker_registration": map[string]string{
			"provider_id": "google_workspace",
			"file_id":     fileID,
			"mime_type":   mime,
		},
	}
	resp := httpJSON(t, "POST", baseURL+"/threat_models/"+tmID+"/documents", jwt, body)
	return resp["id"].(string)
}

func pollAccessStatus(t *testing.T, baseURL, jwt, docID string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	tmID := os.Getenv("TMI_MANUAL_THREAT_MODEL_ID")
	for time.Now().Before(deadline) {
		resp := httpJSON(t, "GET", baseURL+"/threat_models/"+tmID+"/documents/"+docID, jwt, nil)
		if s, _ := resp["access_status"].(string); s != "" && s != "unknown" {
			return s
		}
		time.Sleep(3 * time.Second)
	}
	return "timeout"
}

func deleteContentToken(t *testing.T, baseURL, jwt string) {
	req, err := http.NewRequest("DELETE", baseURL+"/me/content_tokens/google_workspace", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http DELETE: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete failed: %d %s", resp.StatusCode, string(raw))
	}
}
