package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestClientCallbackAllowList_EmptyRejectsEverything pins the fail-closed
// default for T16. If a future refactor flips this to "empty == allow all",
// the open-redirect surface returns.
func TestClientCallbackAllowList_EmptyRejectsEverything(t *testing.T) {
	a := NewClientCallbackAllowList(nil)
	assert.False(t, a.Allowed("http://localhost:8079/"))
	assert.False(t, a.Allowed("https://example.com/cb"))
	assert.False(t, a.Configured())
}

func TestClientCallbackAllowList_ExactMatch(t *testing.T) {
	a := NewClientCallbackAllowList([]string{"http://localhost:8079/cb"})
	assert.True(t, a.Allowed("http://localhost:8079/cb"))
	assert.False(t, a.Allowed("http://localhost:8079/cb?code=x"), "exact-match patterns must not partial-match")
	assert.False(t, a.Allowed("http://localhost:8079/"))
}

func TestClientCallbackAllowList_WildcardSuffix(t *testing.T) {
	a := NewClientCallbackAllowList([]string{"http://localhost:8079/*"})
	assert.True(t, a.Allowed("http://localhost:8079/"))
	assert.True(t, a.Allowed("http://localhost:8079/cb"))
	assert.True(t, a.Allowed("http://localhost:8079/cb?code=abc"))
	assert.False(t, a.Allowed("http://evil.com/cb"))
}

// TestClientCallbackAllowList_RejectsAttackerVariants ensures common
// open-redirect tricks (path traversal, host smuggling) do not slip past.
func TestClientCallbackAllowList_RejectsAttackerVariants(t *testing.T) {
	a := NewClientCallbackAllowList([]string{"http://trusted.example.com/*"})
	cases := []string{
		"http://evil.com/x",
		"http://trusted.example.com.evil.com/",
		"https://trusted.example.com/",  // scheme mismatch
		"http://Trusted.Example.com/",   // case mismatch
		"//trusted.example.com/",        // protocol-relative
		"http:trusted.example.com/path", // malformed scheme
	}
	for _, u := range cases {
		assert.False(t, a.Allowed(u), "attacker variant must be rejected: %s", u)
	}
}

// TestClientCallbackAllowList_TrimsAndDropsEmpties verifies that whitespace
// and empty entries do not silently widen the allowlist.
func TestClientCallbackAllowList_TrimsAndDropsEmpties(t *testing.T) {
	a := NewClientCallbackAllowList([]string{"", "  ", "http://ok/cb"})
	assert.True(t, a.Configured())
	assert.True(t, a.Allowed("http://ok/cb"))
	assert.False(t, a.Allowed(""))
}

// TestClientCallbackAllowList_RejectsUserinfo pins the fix for prefix
// patterns that end at the port: without URL parsing, "http://127.0.0.1:"
// admits "http://127.0.0.1:1@evil.example/", whose real host is evil.example.
func TestClientCallbackAllowList_RejectsUserinfo(t *testing.T) {
	for _, p := range []string{"http://127.0.0.1:*", "http://localhost:8079*", "http://localhost:8079/*"} {
		a := NewClientCallbackAllowList([]string{p})
		for _, u := range []string{
			"http://127.0.0.1:1@evil.example/cb",
			"http://localhost:8079@evil.example/cb",
			"http://user:pass@127.0.0.1:5000/cb",
		} {
			assert.False(t, a.Allowed(u), "pattern %q must reject userinfo URL %q", p, u)
		}
	}
}

// TestClientCallbackAllowList_LoopbackAnyPort covers the RFC 8252 §7.3
// loopback redirect: any port, fixed scheme and host.
func TestClientCallbackAllowList_LoopbackAnyPort(t *testing.T) {
	a := NewClientCallbackAllowList([]string{"http://127.0.0.1:*", "http://[::1]:*/callback"})
	allowed := []string{
		"http://127.0.0.1:53124/callback",
		"http://127.0.0.1:1/",
		"http://127.0.0.1:65000/cb?code=x&state=y",
		"http://[::1]:4000/callback",
		"http://[::1]:4000/callback?code=x",
	}
	for _, u := range allowed {
		assert.True(t, a.Allowed(u), "must allow %q", u)
	}
	rejected := []string{
		"http://127.0.0.1/callback",         // no port
		"https://127.0.0.1:5000/callback",   // scheme mismatch
		"http://127.0.0.2:5000/callback",    // different host
		"http://localhost:5000/callback",    // host not in pattern
		"http://[::1]:4000/other",           // path mismatch
		"http://[::1]:4000/callback/extra",  // exact path only
		"http://127.0.0.1.evil.example:80/", // host smuggling
		"http://evil.example/http://127.0.0.1:1/",
	}
	for _, u := range rejected {
		assert.False(t, a.Allowed(u), "must reject %q", u)
	}
}

// TestClientCallbackAllowList_PortWildcardLoopbackOnly ensures ":*" never
// widens a non-loopback host to every port.
func TestClientCallbackAllowList_PortWildcardLoopbackOnly(t *testing.T) {
	a := NewClientCallbackAllowList([]string{"https://app.example.com:*/cb"})
	assert.False(t, a.Allowed("https://app.example.com:8443/cb"))
	assert.False(t, a.Allowed("https://app.example.com:/cb"))
}

// TestClientCallbackAllowList_LoopbackPathPrefix covers "host:*/prefix*".
func TestClientCallbackAllowList_LoopbackPathPrefix(t *testing.T) {
	a := NewClientCallbackAllowList([]string{"http://localhost:*/oauth/*"})
	assert.True(t, a.Allowed("http://localhost:9000/oauth/callback"))
	assert.False(t, a.Allowed("http://localhost:9000/other"))
}

// TestClientCallbackAllowList_PrefixPinsHost ensures a prefix pattern that
// stops mid-authority cannot be extended into another host.
func TestClientCallbackAllowList_PrefixPinsHost(t *testing.T) {
	a := NewClientCallbackAllowList([]string{"https://app.example.com*", "https://*"})
	assert.True(t, a.Allowed("https://app.example.com/cb"))
	assert.True(t, a.Allowed("https://app.example.com?x=1"))
	assert.False(t, a.Allowed("https://app.example.com.evil.net/cb"))
	assert.False(t, a.Allowed("https://app.example.company/cb"))
	assert.False(t, a.Allowed("https://evil.example/cb"), `"https://*" must not admit every host`)
}
