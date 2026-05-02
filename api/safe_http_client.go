package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HostResolver looks up the IPs for a hostname. Implementations must be
// safe for concurrent use.
type HostResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type defaultResolver struct{}

func (defaultResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// SafeFetchOptions controls a single Fetch / FetchStreaming call.
// Zero values fall back to client-level defaults.
type SafeFetchOptions struct {
	Method                string
	Body                  io.Reader
	Headers               http.Header
	Timeout               time.Duration
	ResponseHeaderTimeout time.Duration
	MaxBodyBytes          int64
	AllowRedirects        bool
	// RejectIfBodyExceedsMax causes Fetch to return ErrSafeHTTPBodyTooLarge
	// when the response carries a Content-Length header that exceeds
	// MaxBodyBytes (or the client default). Without this flag the body
	// is silently truncated. Useful for callers that prefer fast-fail
	// over partial reads (e.g., webhook deliveries where a response
	// larger than the cap is treated as a hostile target).
	RejectIfBodyExceedsMax bool
}

// SafeFetchResult is returned by Fetch.
type SafeFetchResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	Truncated  bool
}

// SafeHTTPClient is the single egress path for server-originated outbound
// HTTP. It validates the request URL against scheme/allowlist/SSRF rules,
// resolves the hostname **once**, walks every resolved IP through the
// blocklist, and pins subsequent dial(s) to the validated IP. This closes
// the DNS-rebinding window between validation and connection that arises
// when callers re-resolve the hostname inside http.Client.
//
// Callers MUST route all server-originated outbound HTTP through a
// SafeHTTPClient. A repository-level lint check enforces this.
type SafeHTTPClient struct {
	validator         *URIValidator
	resolver          HostResolver
	defaultTimeout    time.Duration
	defaultMaxBody    int64
	defaultHeaderWait time.Duration
	maxResponseHdrB   int64
	userAgent         string
	transportWrap     func(http.RoundTripper) http.RoundTripper
	dialer            *net.Dialer
}

// SafeHTTPClientOption configures a SafeHTTPClient.
type SafeHTTPClientOption func(*SafeHTTPClient)

// WithResolver overrides the host resolver. Used in tests to drive
// deterministic resolution.
func WithResolver(r HostResolver) SafeHTTPClientOption {
	return func(c *SafeHTTPClient) { c.resolver = r }
}

// WithUserAgent sets a default User-Agent header applied when the request
// has none.
func WithUserAgent(ua string) SafeHTTPClientOption {
	return func(c *SafeHTTPClient) { c.userAgent = ua }
}

// WithTransportWrapper registers a wrapper around the pinned transport
// (e.g. otelhttp.NewTransport).
func WithTransportWrapper(f func(http.RoundTripper) http.RoundTripper) SafeHTTPClientOption {
	return func(c *SafeHTTPClient) { c.transportWrap = f }
}

// WithDefaultTimeouts overrides the per-fetch defaults applied when
// SafeFetchOptions leaves the corresponding field zero.
func WithDefaultTimeouts(overall, headerWait time.Duration, maxBody int64) SafeHTTPClientOption {
	return func(c *SafeHTTPClient) {
		if overall > 0 {
			c.defaultTimeout = overall
		}
		if headerWait > 0 {
			c.defaultHeaderWait = headerWait
		}
		if maxBody > 0 {
			c.defaultMaxBody = maxBody
		}
	}
}

// NewSafeHTTPClient builds a SafeHTTPClient backed by the given URIValidator.
// The validator's scheme and allowlist policy are reused; this client adds
// the IP-pinning, header timeout, and body-cap controls on top.
func NewSafeHTTPClient(validator *URIValidator, opts ...SafeHTTPClientOption) *SafeHTTPClient {
	c := &SafeHTTPClient{
		validator:         validator,
		resolver:          defaultResolver{},
		defaultTimeout:    30 * time.Second,
		defaultHeaderWait: 10 * time.Second,
		defaultMaxBody:    10 * 1024 * 1024,
		maxResponseHdrB:   64 * 1024,
		dialer:            &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ErrSafeHTTPRedirectBlocked is returned indirectly when redirects are
// disallowed and the upstream tried to redirect; the caller observes a
// 3xx response with no body follow.
var ErrSafeHTTPRedirectBlocked = errors.New("safe_http: redirects are not allowed")

// ErrSafeHTTPBodyTooLarge is returned by Fetch when
// SafeFetchOptions.RejectIfBodyExceedsMax is set and the upstream
// declared a Content-Length larger than the configured MaxBodyBytes.
var ErrSafeHTTPBodyTooLarge = errors.New("safe_http: response body exceeds configured maximum")

// Fetch issues the request and reads the body into memory, capped at
// opts.MaxBodyBytes (or the client default). The body is always returned
// (possibly truncated) when no transport-level error occurred.
//
// When opts.RejectIfBodyExceedsMax is set and the response carries a
// Content-Length header strictly greater than the configured cap, the
// body is not read and the call returns ErrSafeHTTPBodyTooLarge.
func (c *SafeHTTPClient) Fetch(ctx context.Context, rawURL string, opts SafeFetchOptions) (*SafeFetchResult, error) {
	resp, maxBytes, err := c.do(ctx, rawURL, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if opts.RejectIfBodyExceedsMax && maxBytes > 0 && resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("%w: declared Content-Length %d > max %d",
			ErrSafeHTTPBodyTooLarge, resp.ContentLength, maxBytes)
	}

	body, truncated, err := readCappedBody(resp.Body, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	return &SafeFetchResult{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       body,
		Truncated:  truncated,
	}, nil
}

// FetchStreaming issues the request and returns the open *http.Response.
// The response body is wrapped so that reads never exceed opts.MaxBodyBytes.
// The caller MUST close resp.Body.
func (c *SafeHTTPClient) FetchStreaming(ctx context.Context, rawURL string, opts SafeFetchOptions) (*http.Response, error) {
	resp, maxBytes, err := c.do(ctx, rawURL, opts)
	if err != nil {
		return nil, err
	}
	resp.Body = newCappedReadCloser(resp.Body, maxBytes)
	return resp, nil
}

func (c *SafeHTTPClient) do(ctx context.Context, rawURL string, opts SafeFetchOptions) (*http.Response, int64, error) {
	if opts.Method == "" {
		opts.Method = http.MethodGet
	}
	if opts.Timeout <= 0 {
		opts.Timeout = c.defaultTimeout
	}
	if opts.ResponseHeaderTimeout <= 0 {
		opts.ResponseHeaderTimeout = c.defaultHeaderWait
	}
	maxBody := opts.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = c.defaultMaxBody
	}

	pinnedIP, port, _, err := c.resolveAndPin(ctx, rawURL)
	if err != nil {
		return nil, 0, err
	}

	transport := &http.Transport{
		Proxy:                  nil,
		ResponseHeaderTimeout:  opts.ResponseHeaderTimeout,
		MaxResponseHeaderBytes: c.maxResponseHdrB,
		MaxIdleConns:           1,
		MaxIdleConnsPerHost:    1,
		IdleConnTimeout:        30 * time.Second,
		ForceAttemptHTTP2:      true,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		DialContext: func(dctx context.Context, network, _ string) (net.Conn, error) {
			return c.dialer.DialContext(dctx, network, net.JoinHostPort(pinnedIP, port))
		},
	}

	var rt http.RoundTripper = transport
	if c.transportWrap != nil {
		rt = c.transportWrap(rt)
	}

	httpClient := &http.Client{
		Timeout:   opts.Timeout,
		Transport: rt,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if opts.AllowRedirects {
				return nil
			}
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, opts.Method, rawURL, opts.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	if c.userAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	for k, vs := range opts.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := httpClient.Do(req) //nolint:gosec // URL validated and IP-pinned by resolveAndPin above
	if err != nil {
		return nil, 0, err
	}
	return resp, maxBody, nil
}

// resolveAndPin performs scheme + allowlist + IP-block validation on rawURL
// and returns a single IP that the caller MUST dial. Returns the resolved
// IP, the port, and the original hostname (for callers needing SNI / Host
// header values). Localhost names are blocked in open mode.
func (c *SafeHTTPClient) resolveAndPin(ctx context.Context, rawURL string) (pinnedIP, port, host string, err error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", "", fmt.Errorf("invalid URL: missing scheme or host")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if !c.validator.schemes[scheme] {
		allowed := make([]string, 0, len(c.validator.schemes))
		for s := range c.validator.schemes {
			allowed = append(allowed, s)
		}
		return "", "", "", fmt.Errorf("unsupported scheme: %s (allowed: %s)", parsed.Scheme, strings.Join(allowed, ", "))
	}

	host = parsed.Hostname()
	port = parsed.Port()
	if port == "" {
		switch scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		default:
			return "", "", "", fmt.Errorf("cannot infer port for scheme: %s", scheme)
		}
	}

	hostnameLower := strings.ToLower(host)
	allowlistMatch := c.validator.hasAllowlist && c.validator.matchHost(hostnameLower)

	if c.validator.hasAllowlist && !allowlistMatch {
		return "", "", "", fmt.Errorf("host %q is not in allowlist", host)
	}

	if literal := net.ParseIP(host); literal != nil {
		if !allowlistMatch {
			if err := c.validator.checkIP(literal); err != nil {
				return "", "", "", err
			}
		}
		return literal.String(), port, host, nil
	}

	if !c.validator.hasAllowlist {
		if hostnameLower == "localhost" || hostnameLower == "ip6-localhost" || hostnameLower == "ip6-loopback" {
			return "", "", "", fmt.Errorf("blocked: localhost is not allowed")
		}
	}

	ips, err := c.resolver.LookupHost(ctx, host)
	if err != nil {
		return "", "", "", fmt.Errorf("cannot resolve hostname: %s", host)
	}
	if len(ips) == 0 {
		return "", "", "", fmt.Errorf("no IPs resolved for: %s", host)
	}

	if !allowlistMatch {
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip == nil {
				continue
			}
			if err := c.validator.checkIP(ip); err != nil {
				return "", "", "", err
			}
		}
	}

	return ips[0], port, host, nil
}

func readCappedBody(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	if maxBytes <= 0 {
		body, err := io.ReadAll(r)
		return body, false, err
	}
	buf, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > maxBytes {
		return buf[:maxBytes], true, nil
	}
	return buf, false, nil
}

type cappedReadCloser struct {
	r  io.Reader
	rc io.ReadCloser
}

func newCappedReadCloser(rc io.ReadCloser, maxBytes int64) io.ReadCloser {
	if maxBytes <= 0 {
		return rc
	}
	return &cappedReadCloser{r: io.LimitReader(rc, maxBytes), rc: rc}
}

func (c *cappedReadCloser) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *cappedReadCloser) Close() error               { return c.rc.Close() }
