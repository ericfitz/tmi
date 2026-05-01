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
