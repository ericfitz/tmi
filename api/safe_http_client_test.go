package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubResolver is a HostResolver that returns a fixed sequence of IP slices
// for any host, and counts how many times LookupHost was called.
type stubResolver struct {
	calls   atomic.Int32
	results map[string][]string
	err     error
}

func newStubResolver() *stubResolver {
	return &stubResolver{results: map[string][]string{}}
}

func (r *stubResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	r.calls.Add(1)
	if r.err != nil {
		return nil, r.err
	}
	if ips, ok := r.results[host]; ok {
		return ips, nil
	}
	return nil, fmt.Errorf("no stub result for %q", host)
}

// loopbackHostPort starts an httptest.Server listening on 127.0.0.1 and returns
// the port for use with custom resolvers.
func loopbackHostPort(t *testing.T, h http.Handler) (port string, srv *httptest.Server) {
	t.Helper()
	srv = httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return u.Port(), srv
}

// TestSafeHTTPClient_PinsResolvedIP verifies that the helper resolves the
// hostname exactly once and dials the resolved IP, regardless of what the
// system resolver would do at dial time.
//
// This pins the T3 (DNS-rebinding) regression. If a future refactor removes
// the custom DialContext, the dial address will be the original hostname and
// this test will fail because no system resolver will have a binding for the
// fake hostname "rebind.test.invalid".
func TestSafeHTTPClient_PinsResolvedIP(t *testing.T) {
	port, _ := loopbackHostPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))

	resolver := newStubResolver()
	resolver.results["rebind.test.invalid"] = []string{"127.0.0.1"}

	// Allowlist the fake host so the SSRF blocklist (which would reject
	// 127.0.0.1) does not fire — this isolates the IP-pin behavior.
	v := NewURIValidator([]string{"rebind.test.invalid"}, []string{"http"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	uri := fmt.Sprintf("http://rebind.test.invalid:%s/", port)
	res, err := c.Fetch(context.Background(), uri, SafeFetchOptions{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "ok", string(res.Body))
	assert.Equal(t, int32(1), resolver.calls.Load(), "expected exactly one resolution")
}

// TestSafeHTTPClient_BlocksRebindToPrivateIP verifies that when DNS resolves
// to ANY private IP, the request is rejected before any dial. This is the
// canonical SSRF defense; if a future refactor allows partial-block (e.g.
// "first IP wins"), this test fails.
func TestSafeHTTPClient_BlocksRebindToPrivateIP(t *testing.T) {
	resolver := newStubResolver()
	// Resolver returns a public IP and a private IP. ALL must be checked.
	resolver.results["rebind.test.invalid"] = []string{"8.8.8.8", "10.0.0.1"}

	v := NewURIValidator(nil, []string{"http", "https"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	_, err := c.Fetch(context.Background(), "https://rebind.test.invalid/", SafeFetchOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private")
}

// TestSafeHTTPClient_BlocksLiteralPrivateIP verifies that direct literal
// private IPs are rejected (no allowlist).
func TestSafeHTTPClient_BlocksLiteralPrivateIP(t *testing.T) {
	v := NewURIValidator(nil, []string{"http", "https"})
	c := NewSafeHTTPClient(v)

	tests := []struct {
		name string
		url  string
	}{
		{"loopback ipv4", "http://127.0.0.1/path"},
		{"private 10.x", "http://10.0.0.1/path"},
		{"private 192.168.x", "http://192.168.1.1/path"},
		{"link-local", "http://169.254.1.1/path"},
		{"cloud metadata", "http://169.254.169.254/latest/meta-data/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.Fetch(context.Background(), tt.url, SafeFetchOptions{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "blocked")
		})
	}
}

// TestSafeHTTPClient_BlocksLocalhostHostname verifies that the symbolic
// hostname "localhost" is blocked even though it is not a literal IP.
func TestSafeHTTPClient_BlocksLocalhostHostname(t *testing.T) {
	v := NewURIValidator(nil, []string{"http", "https"})
	c := NewSafeHTTPClient(v)

	for _, host := range []string{"localhost", "LOCALHOST", "ip6-localhost", "ip6-loopback"} {
		t.Run(host, func(t *testing.T) {
			_, err := c.Fetch(context.Background(), "http://"+host+"/", SafeFetchOptions{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "blocked")
		})
	}
}

// TestSafeHTTPClient_RedirectNotFollowed verifies the default no-redirect
// policy. If a future refactor flips this default, untrusted servers can
// redirect us to private IPs after the IP pin has been computed.
func TestSafeHTTPClient_RedirectNotFollowed(t *testing.T) {
	port, _ := loopbackHostPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/private", http.StatusFound)
	}))

	resolver := newStubResolver()
	resolver.results["test.invalid"] = []string{"127.0.0.1"}

	v := NewURIValidator([]string{"test.invalid"}, []string{"http"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	res, err := c.Fetch(context.Background(), fmt.Sprintf("http://test.invalid:%s/", port), SafeFetchOptions{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, res.StatusCode)
	// Key invariant: we received the 3xx response itself (Location header
	// reflects the redirect target), proving the client did not follow it.
	assert.Equal(t, "http://10.0.0.1/private", res.Header.Get("Location"))
}

// TestSafeHTTPClient_BodyCapTruncates verifies that responses larger than
// MaxBodyBytes are truncated and Truncated is set.
func TestSafeHTTPClient_BodyCapTruncates(t *testing.T) {
	port, _ := loopbackHostPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 2048)))
	}))

	resolver := newStubResolver()
	resolver.results["test.invalid"] = []string{"127.0.0.1"}

	v := NewURIValidator([]string{"test.invalid"}, []string{"http"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	res, err := c.Fetch(context.Background(), fmt.Sprintf("http://test.invalid:%s/", port), SafeFetchOptions{
		MaxBodyBytes: 256,
	})
	require.NoError(t, err)
	assert.Equal(t, 256, len(res.Body))
	assert.True(t, res.Truncated)
}

// TestSafeHTTPClient_ResponseHeaderTimeout verifies that a server which holds
// the response-header phase open longer than ResponseHeaderTimeout produces
// an error before the per-call Timeout would fire. Defends against
// slow-loris-on-headers (T26).
func TestSafeHTTPClient_ResponseHeaderTimeout(t *testing.T) {
	port, _ := loopbackHostPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep before writing the header.
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	resolver := newStubResolver()
	resolver.results["test.invalid"] = []string{"127.0.0.1"}

	v := NewURIValidator([]string{"test.invalid"}, []string{"http"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	_, err := c.Fetch(context.Background(), fmt.Sprintf("http://test.invalid:%s/", port), SafeFetchOptions{
		Timeout:               2 * time.Second,
		ResponseHeaderTimeout: 50 * time.Millisecond,
	})
	require.Error(t, err)
}

// TestSafeHTTPClient_RejectIfBodyExceedsMax_DeclaredContentLength pins
// that when the upstream advertises a Content-Length larger than the
// configured cap and RejectIfBodyExceedsMax is set, the body is not
// read and ErrSafeHTTPBodyTooLarge is returned. Defends webhook
// delivery against hostile receivers that promise a multi-gigabyte
// body to tie up the worker's read loop (T26).
func TestSafeHTTPClient_RejectIfBodyExceedsMax_DeclaredContentLength(t *testing.T) {
	read := atomic.Int32{}
	port, _ := loopbackHostPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000000")
		w.WriteHeader(http.StatusOK)
		// We start writing — the client must hang up after the header
		// inspection without consuming the bulk of this body.
		buf := make([]byte, 4096)
		for i := 0; i < 100; i++ {
			if _, err := w.Write(buf); err != nil {
				return
			}
			read.Add(int32(len(buf)))
		}
	}))

	resolver := newStubResolver()
	resolver.results["test.invalid"] = []string{"127.0.0.1"}

	v := NewURIValidator([]string{"test.invalid"}, []string{"http"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	_, err := c.Fetch(context.Background(), fmt.Sprintf("http://test.invalid:%s/", port), SafeFetchOptions{
		MaxBodyBytes:           1024,
		RejectIfBodyExceedsMax: true,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSafeHTTPBodyTooLarge), "want ErrSafeHTTPBodyTooLarge, got %v", err)
}

// TestSafeHTTPClient_RejectIfBodyExceedsMax_UnknownLengthFallsThrough
// pins that without a Content-Length the request still completes (the
// pre-read check applies only to declared lengths). Truncation behaves
// as before.
func TestSafeHTTPClient_RejectIfBodyExceedsMax_UnknownLengthFallsThrough(t *testing.T) {
	port, _ := loopbackHostPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Chunked → no Content-Length set.
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}))

	resolver := newStubResolver()
	resolver.results["test.invalid"] = []string{"127.0.0.1"}

	v := NewURIValidator([]string{"test.invalid"}, []string{"http"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	res, err := c.Fetch(context.Background(), fmt.Sprintf("http://test.invalid:%s/", port), SafeFetchOptions{
		MaxBodyBytes:           1024,
		RejectIfBodyExceedsMax: true,
	})
	require.NoError(t, err, "chunked transfer with no Content-Length must not be pre-rejected")
	assert.Equal(t, 1024, len(res.Body))
	assert.True(t, res.Truncated)
}

// TestSafeHTTPClient_HostNotInAllowlist verifies that hostnames outside the
// allowlist are rejected with an "allowlist" error.
func TestSafeHTTPClient_HostNotInAllowlist(t *testing.T) {
	v := NewURIValidator([]string{"trusted.example.com"}, []string{"https"})
	c := NewSafeHTTPClient(v)

	_, err := c.Fetch(context.Background(), "https://other.example.com/", SafeFetchOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allowlist")
}

// TestSafeHTTPClient_SchemeRejected verifies that URLs with unsupported
// schemes are rejected before any DNS resolution.
func TestSafeHTTPClient_SchemeRejected(t *testing.T) {
	resolver := newStubResolver()
	v := NewURIValidator(nil, []string{"https"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	_, err := c.Fetch(context.Background(), "ftp://example.com/", SafeFetchOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme")
	assert.Equal(t, int32(0), resolver.calls.Load(), "scheme check must run before DNS")
}

// TestSafeHTTPClient_ResolverError verifies that a DNS lookup failure produces
// a clean error (not a panic, not a fallthrough to dial).
func TestSafeHTTPClient_ResolverError(t *testing.T) {
	resolver := newStubResolver()
	resolver.err = errors.New("synthetic dns failure")

	v := NewURIValidator(nil, []string{"https"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	_, err := c.Fetch(context.Background(), "https://anywhere.example.com/", SafeFetchOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve")
}

// TestSafeHTTPClient_FetchStreaming verifies that FetchStreaming returns an
// open response whose body reads are bounded.
func TestSafeHTTPClient_FetchStreaming(t *testing.T) {
	port, _ := loopbackHostPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}))

	resolver := newStubResolver()
	resolver.results["test.invalid"] = []string{"127.0.0.1"}

	v := NewURIValidator([]string{"test.invalid"}, []string{"http"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	resp, err := c.FetchStreaming(context.Background(), fmt.Sprintf("http://test.invalid:%s/", port), SafeFetchOptions{
		MaxBodyBytes: 1024,
	})
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	buf := make([]byte, 8192)
	n, err := readFully(resp.Body, buf)
	require.NoError(t, err)
	assert.Equal(t, 1024, n, "stream must be capped at MaxBodyBytes")
}

// readFully reads until EOF or buf is exhausted.
func readFully(r interface {
	Read(p []byte) (int, error)
}, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if errorsIsEOF(err) {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}

func errorsIsEOF(err error) bool {
	return err != nil && err.Error() == "EOF"
}

// TestSafeHTTPClient_AllowlistedHostBypassesIPCheck verifies that a host on
// the allowlist that resolves to a normally-blocked IP (e.g., internal corp
// host on RFC1918) is permitted. This preserves the existing URIValidator
// semantics where allowlisted hosts trust the operator.
func TestSafeHTTPClient_AllowlistedHostBypassesIPCheck(t *testing.T) {
	port, _ := loopbackHostPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	resolver := newStubResolver()
	resolver.results["internal.corp"] = []string{"127.0.0.1"}

	v := NewURIValidator([]string{"internal.corp"}, []string{"http"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	res, err := c.Fetch(context.Background(), fmt.Sprintf("http://internal.corp:%s/", port), SafeFetchOptions{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

// TestSafeHTTPClient_DialAddressIgnored is a behavioral guard: even if the
// stdlib were to compute an unexpected dial address from URL.Host, the
// pinned DialContext ignores the address argument and uses the validated
// IP. We verify this by setting up a server on 127.0.0.1 and using a
// hostname that would not resolve via the system resolver.
func TestSafeHTTPClient_DialAddressIgnored(t *testing.T) {
	port, _ := loopbackHostPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	resolver := newStubResolver()
	resolver.results["never-resolves.invalid"] = []string{"127.0.0.1"}

	v := NewURIValidator([]string{"never-resolves.invalid"}, []string{"http"})
	c := NewSafeHTTPClient(v, WithResolver(resolver))

	uri := fmt.Sprintf("http://never-resolves.invalid:%s/path", port)
	res, err := c.Fetch(context.Background(), uri, SafeFetchOptions{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	// Sanity: the system resolver should not actually know this hostname.
	_, sysErr := net.DefaultResolver.LookupHost(context.Background(), "never-resolves.invalid")
	assert.Error(t, sysErr, "system resolver should not know the fake hostname")
}
