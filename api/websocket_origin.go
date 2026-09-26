package api

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// withOriginCheck returns a copy of u whose CheckOrigin validates the
// request's Origin with the server settings carried on the gin context.
// SEM@95fbc20511ce80f464d07e7723483035dbf87346: attach a gin-context-aware Origin check to a WebSocket upgrader (pure)
func withOriginCheck(c *gin.Context, u websocket.Upgrader) *websocket.Upgrader {
	u.CheckOrigin = func(r *http.Request) bool { return checkWebSocketOrigin(c, r) }
	return &u
}

// checkWebSocketOrigin accepts a missing Origin (non-browser clients), any
// origin in dev mode, and otherwise only an http(s) origin whose host exactly
// equals a loopback host (any port), the request's Host, or the TLS subject
// name, or whose scheme and host equal an entry of the CORS allowed origins
// or WEBSOCKET_ALLOWED_ORIGINS. Origins are parsed, never prefix-matched, so
// "https://api.tmi.dev.evil.example" does not pass as "https://api.tmi.dev".
// SEM@95fbc20511ce80f464d07e7723483035dbf87346: validate a WebSocket upgrade's Origin against loopback, Host, TLS name, and configured origins (pure)
func checkWebSocketOrigin(c *gin.Context, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if isDev, _ := c.Get("isDev"); isDev == true {
		return true
	}
	tlsSubjectName := c.GetString("tlsSubjectName")
	configured := c.GetStringSlice("corsAllowedOrigins")
	configured = append(configured, strings.Split(os.Getenv("WEBSOCKET_ALLOWED_ORIGINS"), ",")...)
	if originAllowed(origin, r.Host, tlsSubjectName, configured) {
		return true
	}
	slogging.Get().Warn("Rejected WebSocket connection from origin: %s", origin)
	return false
}

// originAllowed reports whether a browser Origin matches the allowed set by
// exact scheme and host comparison.
// SEM@95fbc20511ce80f464d07e7723483035dbf87346: match a browser Origin against allowed hosts by exact scheme and host (pure)
func originAllowed(origin, requestHost, tlsSubjectName string, configured []string) bool {
	o, ok := parseOrigin(origin)
	if !ok {
		return false
	}
	if isLoopbackName(o.Hostname()) {
		return true
	}
	for _, host := range []string{requestHost, tlsSubjectName} {
		if host != "" && strings.EqualFold(o.Host, host) {
			return true
		}
	}
	for _, entry := range configured {
		if a, ok := parseOrigin(strings.TrimSpace(entry)); ok && a.Scheme == o.Scheme && strings.EqualFold(a.Host, o.Host) {
			return true
		}
	}
	return false
}

// parseOrigin parses an http(s) origin, rejecting userinfo and empty hosts.
// SEM@95fbc20511ce80f464d07e7723483035dbf87346: parse an http(s) origin string, rejecting userinfo and missing host (pure)
func parseOrigin(s string) (*url.URL, bool) {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, false
	}
	return u, true
}

// isLoopbackName reports whether host is localhost or a loopback IP.
// SEM@95fbc20511ce80f464d07e7723483035dbf87346: report whether a hostname is localhost or a loopback IP (pure)
func isLoopbackName(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
