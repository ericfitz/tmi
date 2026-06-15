package safehttp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubResolver returns a fixed IP slice for any configured host.
type stubResolver struct {
	results map[string][]string
}

func (r stubResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if ips, ok := r.results[host]; ok {
		return ips, nil
	}
	return nil, fmt.Errorf("no stub result for %q", host)
}

func TestCheckIP(t *testing.T) {
	blocked := map[string]string{
		"127.0.0.1":       "loopback",
		"::1":             "loopback",
		"10.0.0.1":        "private",
		"172.16.5.4":      "private",
		"192.168.1.1":     "private",
		"169.254.169.254": "cloud metadata",
		"169.254.1.1":     "link-local",
		"fe80::1":         "link-local",
	}
	for ipStr, want := range blocked {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			t.Fatalf("bad test IP %q", ipStr)
		}
		err := CheckIP(ip)
		if err == nil {
			t.Errorf("CheckIP(%s) = nil, want error containing %q", ipStr, want)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("CheckIP(%s) = %q, want substring %q", ipStr, err, want)
		}
	}

	for _, ipStr := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"} {
		if err := CheckIP(net.ParseIP(ipStr)); err != nil {
			t.Errorf("CheckIP(%s) = %v, want nil (public address)", ipStr, err)
		}
	}
}

func TestIsBlockedLocalhostName(t *testing.T) {
	for _, h := range []string{"localhost", "LOCALHOST", "ip6-localhost", "ip6-loopback"} {
		if !IsBlockedLocalhostName(h) {
			t.Errorf("IsBlockedLocalhostName(%q) = false, want true", h)
		}
	}
	for _, h := range []string{"example.com", "localhost.example.com", "8.8.8.8"} {
		if IsBlockedLocalhostName(h) {
			t.Errorf("IsBlockedLocalhostName(%q) = true, want false", h)
		}
	}
}

func TestPinningDialer_BlocksLiteralBlockedIP(t *testing.T) {
	d := NewPinningDialer(nil, nil, time.Second)
	cases := map[string]string{
		"127.0.0.1:80":       "loopback",
		"10.0.0.1:80":        "private",
		"192.168.1.1:443":    "private",
		"169.254.169.254:80": "cloud metadata",
		"169.254.1.1:80":     "link-local",
	}
	for addr, want := range cases {
		conn, err := d.DialContext(context.Background(), "tcp", addr)
		if err == nil {
			_ = conn.Close()
			t.Errorf("DialContext(%s) = nil error, want %q", addr, want)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("DialContext(%s) = %q, want substring %q", addr, err, want)
		}
	}
}

func TestPinningDialer_BlocksRebindToPrivateIP(t *testing.T) {
	// A hostname resolving to a public AND a private IP must be rejected — no
	// "first public IP wins" partial bypass.
	d := NewPinningDialer(stubResolver{results: map[string][]string{
		"rebind.invalid": {"8.8.8.8", "10.0.0.1"},
	}}, nil, time.Second)

	_, err := d.DialContext(context.Background(), "tcp", "rebind.invalid:443")
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("DialContext(rebind.invalid) = %v, want private-address block", err)
	}
}

func TestPinningDialer_BlocksLocalhostName(t *testing.T) {
	d := NewPinningDialer(nil, nil, time.Second)
	_, err := d.DialContext(context.Background(), "tcp", "localhost:80")
	if err == nil || !strings.Contains(err.Error(), "localhost is not allowed") {
		t.Fatalf("DialContext(localhost) = %v, want localhost block", err)
	}
}

func TestNewHardenedClient_BlocksLoopbackByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewHardenedClient(HardenedClientOptions{Timeout: 2 * time.Second})
	resp, err := client.Get(srv.URL) // srv.URL is http://127.0.0.1:PORT
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("Get(%s) = %v, want loopback block", srv.URL, err)
	}
}

func TestNewHardenedClient_AllowHostReachesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := NewHardenedClient(HardenedClientOptions{
		Timeout: 2 * time.Second,
		AllowHost: func(host string) bool {
			ip := net.ParseIP(host)
			return ip != nil && ip.IsLoopback()
		},
	})
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get(%s) with loopback allow = %v, want success", srv.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestNewHardenedClient_RefusesRedirects(t *testing.T) {
	var targetHits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	client := NewHardenedClient(HardenedClientOptions{
		Timeout: 2 * time.Second,
		AllowHost: func(host string) bool {
			ip := net.ParseIP(host)
			return ip != nil && ip.IsLoopback()
		},
	})
	resp, err := client.Get(redirector.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "refusing to follow redirect") {
		t.Fatalf("Get(redirector) = %v, want redirect refusal", err)
	}
	if targetHits != 0 {
		t.Errorf("redirect target reached %d times, want 0", targetHits)
	}
}
