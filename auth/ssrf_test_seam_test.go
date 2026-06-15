package auth

import (
	"net"

	"github.com/ericfitz/tmi/internal/safehttp"
)

// init wires the test seam so provider HTTP clients built in the auth test
// binary can reach loopback httptest servers. Production leaves dialAllowHost
// nil, so the full SSRF blocklist (loopback/private/link-local/cloud-metadata)
// is enforced at dial time. Only loopback is allowed here — private,
// link-local, and cloud-metadata targets remain blocked, so the dial-block
// regression tests (provider_ssrf_test.go) still prove the SSRF defense.
func init() {
	dialAllowHost = func(host string) bool {
		if ip := net.ParseIP(host); ip != nil {
			return ip.IsLoopback()
		}
		return safehttp.IsBlockedLocalhostName(host)
	}
}
