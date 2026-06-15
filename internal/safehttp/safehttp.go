// Package safehttp provides the shared SSRF-validation and dial-time
// IP-pinning core for server-originated outbound HTTP.
//
// It lives below both api/ and auth/ in the import graph (it imports only the
// standard library) so that both packages can share one SSRF blocklist and one
// pinning dialer. auth/ cannot import api/ (api already imports auth), which is
// why this core is extracted here instead of living in api/.
//
// Two consumers:
//
//   - api/ reuses CheckIP / IsBlockedLocalhostName as the single source of
//     truth for the SSRF IP blocklist used by its URIValidator and
//     SafeHTTPClient.
//   - auth/ (OAuth/OIDC provider clients, OIDC discovery, SAML metadata fetch)
//     builds its outbound *http.Client via NewHardenedClient, which pins the
//     dialed IP for every request so an admin-set internal token_url /
//     userinfo / issuer / metadata URL is blocked at dial time.
package safehttp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// HostResolver looks up the IPs for a hostname. Implementations must be safe
// for concurrent use.
type HostResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type defaultResolver struct{}

func (defaultResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// CheckIP returns a non-nil error when ip falls in a range that
// server-originated outbound HTTP must never reach: loopback, RFC1918 private
// space, the cloud-metadata endpoint (169.254.169.254), or any other
// link-local address. It is the single source of truth for the SSRF IP
// blocklist shared by api/ and auth/.
func CheckIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("blocked: loopback address %s", ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("blocked: private address %s", ip)
	}
	// Cloud metadata endpoint (AWS, GCP, Azure) — check before link-local
	// since 169.254.169.254 is in the link-local range but deserves a
	// specific error.
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("blocked: cloud metadata endpoint %s", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("blocked: link-local address %s", ip)
	}
	return nil
}

// IsBlockedLocalhostName reports whether host is a textual localhost alias that
// must be blocked before DNS resolution. These usually resolve to loopback (and
// would be caught by CheckIP), but blocking by name is defense-in-depth against
// a resolver that maps them elsewhere.
func IsBlockedLocalhostName(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "ip6-localhost", "ip6-loopback":
		return true
	}
	return false
}

// PinningDialer is an http.Transport DialContext that validates the dial target
// against the SSRF blocklist and pins the connection to a validated IP. For a
// hostname it resolves once, rejects the dial if ANY resolved IP is blocked,
// and dials the first resolved IP — closing the validate/connect DNS-rebinding
// window. For a literal IP it validates directly.
type PinningDialer struct {
	resolver  HostResolver
	dialer    *net.Dialer
	allowHost func(host string) bool
}

// NewPinningDialer builds a PinningDialer. resolver defaults to the system
// resolver when nil. allowHost, when non-nil and returning true for a dialed
// host, bypasses the SSRF blocklist for that host; it is intended only for
// tests that must reach a loopback httptest server and is never set in
// production. dialTimeout defaults to 10s when non-positive.
func NewPinningDialer(resolver HostResolver, allowHost func(host string) bool, dialTimeout time.Duration) *PinningDialer {
	if resolver == nil {
		resolver = defaultResolver{}
	}
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}
	return &PinningDialer{
		resolver:  resolver,
		dialer:    &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second},
		allowHost: allowHost,
	}
}

// DialContext validates network/addr and dials a pinned, blocklist-cleared IP.
func (d *PinningDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("safehttp: invalid dial address %q: %w", addr, err)
	}

	if d.allowHost != nil && d.allowHost(host) {
		return d.dialer.DialContext(ctx, network, addr)
	}

	// Literal IP target — validate and dial directly.
	if ip := net.ParseIP(host); ip != nil {
		if err := CheckIP(ip); err != nil {
			return nil, err
		}
		return d.dialer.DialContext(ctx, network, addr)
	}

	if IsBlockedLocalhostName(host) {
		return nil, fmt.Errorf("blocked: localhost is not allowed")
	}

	ips, err := d.resolver.LookupHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve hostname: %s", host)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPs resolved for: %s", host)
	}
	// Every resolved IP must clear the blocklist; a single blocked IP rejects
	// the dial (no "first public IP wins" partial bypass).
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if err := CheckIP(ip); err != nil {
			return nil, err
		}
	}
	// Pin to the first resolved IP so the connection cannot be rebound to a
	// different address between validation and dial.
	return d.dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
}

// RefuseRedirects is an http.Client CheckRedirect policy that blocks every
// redirect. Provider token/userinfo/issuer and SAML metadata URLs are
// runtime-mutable settings; following a redirect (Go re-sends the request body
// on 307/308) would let a hostile or compromised endpoint bounce a
// secret-bearing request to an internal or attacker-chosen target (SSRF).
func RefuseRedirects(req *http.Request, _ []*http.Request) error {
	return fmt.Errorf("refusing to follow redirect to %s: outbound provider endpoints must not redirect", req.URL.Redacted())
}

// HardenedClientOptions configures NewHardenedClient.
type HardenedClientOptions struct {
	// Timeout is the overall per-request timeout (http.Client.Timeout). A
	// non-positive value leaves the client with no overall timeout (callers
	// should always set one).
	Timeout time.Duration
	// DialTimeout bounds a single dial attempt. Defaults to 10s.
	DialTimeout time.Duration
	// Resolver overrides DNS resolution. Defaults to the system resolver.
	Resolver HostResolver
	// TransportWrap, when non-nil, wraps the pinning transport (e.g.
	// otelhttp.NewTransport) for instrumentation.
	TransportWrap func(http.RoundTripper) http.RoundTripper
	// AllowHost, when non-nil and returning true for a dialed host, bypasses
	// the SSRF blocklist for that host. Intended ONLY for tests that must reach
	// a loopback httptest server; production callers leave it nil.
	AllowHost func(host string) bool
}

// NewHardenedClient builds an *http.Client whose transport validates and pins
// the dialed IP for every request (SSRF defense at dial time) and which refuses
// all redirects. It is the shared egress path for auth/ provider, OIDC
// discovery, and SAML-metadata outbound HTTP, where the destination URL is an
// admin-set, runtime-mutable setting.
//
// Proxy support is intentionally disabled: routing through an HTTP proxy would
// resolve the target host at the proxy and defeat dial-time IP pinning,
// reopening the SSRF hole this client closes.
func NewHardenedClient(opts HardenedClientOptions) *http.Client {
	pd := NewPinningDialer(opts.Resolver, opts.AllowHost, opts.DialTimeout)

	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           pd.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}

	var rt http.RoundTripper = transport
	if opts.TransportWrap != nil {
		rt = opts.TransportWrap(rt)
	}

	return &http.Client{
		Timeout:       opts.Timeout,
		Transport:     rt,
		CheckRedirect: RefuseRedirects,
	}
}
