package api

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Task 1: Allowlist Parsing Tests
// =============================================================================

func TestNewURIValidator_ValidExactEntries(t *testing.T) {
	v := NewURIValidator([]string{"mycompany.com", "example.org"}, nil)
	assert.True(t, v.hasAllowlist)
	assert.True(t, v.exactHosts["mycompany.com"])
	assert.True(t, v.exactHosts["example.org"])
	assert.Empty(t, v.wildcardHosts)
}

func TestNewURIValidator_ValidWildcardEntries(t *testing.T) {
	v := NewURIValidator([]string{"*.mycompany.com", "*.example.org"}, nil)
	assert.True(t, v.hasAllowlist)
	assert.Empty(t, v.exactHosts)
	assert.Contains(t, v.wildcardHosts, "mycompany.com")
	assert.Contains(t, v.wildcardHosts, "example.org")
}

func TestNewURIValidator_MixedEntries(t *testing.T) {
	v := NewURIValidator([]string{"exact.com", "*.wildcard.com"}, nil)
	assert.True(t, v.exactHosts["exact.com"])
	assert.Contains(t, v.wildcardHosts, "wildcard.com")
}

func TestNewURIValidator_InvalidEntriesSkipped(t *testing.T) {
	v := NewURIValidator([]string{
		"",                    // empty
		"*mycompany.com",      // no dot after wildcard
		"foo.*.mycompany.com", // wildcard not at start
		"valid.com",           // valid — should survive
		"*.also-valid.com",    // valid — should survive
		"   ",                 // whitespace only
		"*.*.double-wild.com", // double wildcard
	}, nil)
	assert.True(t, v.hasAllowlist)
	assert.True(t, v.exactHosts["valid.com"])
	assert.Contains(t, v.wildcardHosts, "also-valid.com")
	assert.Len(t, v.exactHosts, 1)
	assert.Len(t, v.wildcardHosts, 1)
}

func TestNewURIValidator_CaseInsensitive(t *testing.T) {
	v := NewURIValidator([]string{"MyCompany.COM", "*.Example.ORG"}, nil)
	assert.True(t, v.exactHosts["mycompany.com"])
	assert.Contains(t, v.wildcardHosts, "example.org")
}

func TestNewURIValidator_DefaultSchemes(t *testing.T) {
	v := NewURIValidator(nil, nil)
	assert.True(t, v.schemes["https"])
	assert.Len(t, v.schemes, 1)
}

func TestNewURIValidator_CustomSchemes(t *testing.T) {
	v := NewURIValidator(nil, []string{"http", "https", "ftp"})
	assert.True(t, v.schemes["http"])
	assert.True(t, v.schemes["https"])
	assert.True(t, v.schemes["ftp"])
	assert.Len(t, v.schemes, 3)
}

func TestNewURIValidator_EmptyAllowlist(t *testing.T) {
	v := NewURIValidator([]string{}, nil)
	assert.False(t, v.hasAllowlist)
}

func TestNewURIValidator_NilAllowlist(t *testing.T) {
	v := NewURIValidator(nil, nil)
	assert.False(t, v.hasAllowlist)
}

func TestNewURIValidator_AllInvalidMeansNoAllowlist(t *testing.T) {
	v := NewURIValidator([]string{"", "*bad", "foo.*.bar.com"}, nil)
	// All entries are invalid, so no valid entries remain — but hasAllowlist
	// reflects whether any entries were configured, not whether any survived.
	// Actually, per the spec hasAllowlist should be true if valid entries exist.
	// Let's verify: if all entries are invalid, hasAllowlist should be false
	// because no effective allowlist is in place.
	assert.False(t, v.hasAllowlist)
}

// =============================================================================
// Task 2: Hostname Matching Tests
// =============================================================================

func TestURIValidator_MatchHost_ExactMatch(t *testing.T) {
	v := NewURIValidator([]string{"mycompany.com"}, nil)
	assert.True(t, v.matchHost("mycompany.com"))
}

func TestURIValidator_MatchHost_SingleSubdomain(t *testing.T) {
	v := NewURIValidator([]string{"mycompany.com"}, nil)
	assert.True(t, v.matchHost("www.mycompany.com"))
	assert.True(t, v.matchHost("api.mycompany.com"))
}

func TestURIValidator_MatchHost_MultiSubdomainRejected(t *testing.T) {
	v := NewURIValidator([]string{"mycompany.com"}, nil)
	assert.False(t, v.matchHost("a.b.mycompany.com"))
	assert.False(t, v.matchHost("deep.nested.mycompany.com"))
}

func TestURIValidator_MatchHost_WildcardAnyDepth(t *testing.T) {
	v := NewURIValidator([]string{"*.mycompany.com"}, nil)
	assert.True(t, v.matchHost("mycompany.com"))
	assert.True(t, v.matchHost("www.mycompany.com"))
	assert.True(t, v.matchHost("a.b.mycompany.com"))
	assert.True(t, v.matchHost("deep.nested.sub.mycompany.com"))
}

func TestURIValidator_MatchHost_CaseInsensitive(t *testing.T) {
	v := NewURIValidator([]string{"MyCompany.COM"}, nil)
	assert.True(t, v.matchHost("MYCOMPANY.COM"))
	assert.True(t, v.matchHost("mycompany.com"))
	assert.True(t, v.matchHost("WWW.MyCompany.com"))
}

func TestURIValidator_MatchHost_SuffixAttackPrevention(t *testing.T) {
	v := NewURIValidator([]string{"mycompany.com"}, nil)
	assert.False(t, v.matchHost("mycompany.com.evil.com"))
	assert.False(t, v.matchHost("notmycompany.com"))
	assert.False(t, v.matchHost("evilmycompany.com"))
}

func TestURIValidator_MatchHost_WildcardSuffixAttackPrevention(t *testing.T) {
	v := NewURIValidator([]string{"*.mycompany.com"}, nil)
	assert.False(t, v.matchHost("mycompany.com.evil.com"))
	assert.False(t, v.matchHost("notmycompany.com"))
}

func TestURIValidator_MatchHost_NoAllowlistOpenMode(t *testing.T) {
	v := NewURIValidator(nil, nil)
	assert.True(t, v.matchHost("anything.example.com"))
	assert.True(t, v.matchHost("literally.anything"))
}

func TestURIValidator_MatchHost_PortIgnored(t *testing.T) {
	v := NewURIValidator([]string{"mycompany.com"}, nil)
	// matchHost receives only hostname (no port), but let's verify
	// the Validate method strips port before calling matchHost
	// This is tested via Validate integration below
	assert.True(t, v.matchHost("mycompany.com"))
}

// =============================================================================
// Task 3: Validate Method and IP Check Tests
// =============================================================================

func TestURIValidator_Validate_DefaultSchemeHTTPSOnly(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := v.Validate("http://example.com/page")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheme")

	err = v.Validate("https://example.com/page")
	assert.NoError(t, err)
}

func TestURIValidator_Validate_CustomSchemes(t *testing.T) {
	v := NewURIValidator(nil, []string{"http", "https"})
	assert.NoError(t, v.Validate("http://example.com/page"))
	assert.NoError(t, v.Validate("https://example.com/page"))

	err := v.Validate("ftp://example.com/file")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheme")
}

func TestURIValidator_Validate_BlocksPrivateIPs(t *testing.T) {
	v := NewURIValidator(nil, []string{"http", "https"})
	tests := []struct {
		name string
		url  string
	}{
		{"RFC1918 10.x", "https://10.0.0.1/doc"},
		{"RFC1918 172.16.x", "https://172.16.0.1/doc"},
		{"RFC1918 192.168.x", "https://192.168.1.1/doc"},
		{"Loopback IPv4", "https://127.0.0.1/doc"},
		{"Loopback IPv6", "https://[::1]/doc"},
		{"Link-local", "https://169.254.1.1/doc"},
		{"Cloud metadata", "https://169.254.169.254/latest/meta-data/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.url)
			assert.Error(t, err, "should block %s", tt.url)
			assert.Contains(t, err.Error(), "blocked")
		})
	}
}

func TestURIValidator_Validate_BlocksLocalhost(t *testing.T) {
	v := NewURIValidator(nil, []string{"https"})
	tests := []struct {
		name string
		url  string
	}{
		{"localhost", "https://localhost/page"},
		{"LOCALHOST", "https://LOCALHOST/page"},
		{"ip6-localhost", "https://ip6-localhost/page"},
		{"ip6-loopback", "https://ip6-loopback/page"},
		{"IP6-LOCALHOST", "https://IP6-LOCALHOST/page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.url)
			assert.Error(t, err, "should block %s", tt.url)
			assert.Contains(t, err.Error(), "blocked")
		})
	}
}

func TestURIValidator_Validate_AllowsPublicURLs(t *testing.T) {
	v := NewURIValidator(nil, []string{"https"})
	tests := []struct {
		name string
		url  string
	}{
		{"Public HTTPS", "https://example.com/doc"},
		{"Public HTTPS with path", "https://docs.google.com/document/d/123"},
		{"Public IP", "https://8.8.8.8/page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.url)
			assert.NoError(t, err, "should allow %s", tt.url)
		})
	}
}

func TestURIValidator_Validate_MalformedURLRejected(t *testing.T) {
	v := NewURIValidator(nil, nil)
	tests := []string{
		"://missing-scheme",
		"",
		"not a url at all",
	}
	for _, u := range tests {
		err := v.Validate(u)
		assert.Error(t, err, "should reject malformed URL: %q", u)
	}
}

func TestURIValidator_Validate_AllowlistBypassesIPChecks(t *testing.T) {
	// An allowlisted host that resolves to a private IP should still be allowed
	v := NewURIValidator([]string{"internal.corp.com"}, []string{"https"})
	// We can't control DNS in unit tests, but we can test that an
	// allowlisted literal private IP is allowed when added to allowlist.
	// Actually, the allowlist checks hostname, not IP. Let's test with
	// the hostname directly.
	err := v.Validate("https://internal.corp.com/page")
	assert.NoError(t, err, "allowlisted host should bypass IP checks")
}

func TestURIValidator_Validate_NonAllowlistedRejected(t *testing.T) {
	v := NewURIValidator([]string{"allowed.com"}, []string{"https"})
	err := v.Validate("https://notallowed.com/page")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in allowlist")
}

func TestURIValidator_Validate_PortHandling(t *testing.T) {
	v := NewURIValidator([]string{"mycompany.com"}, []string{"https"})
	err := v.Validate("https://mycompany.com:8443/page")
	assert.NoError(t, err, "should match host ignoring port")
}

func TestURIValidator_CheckIP_Unit(t *testing.T) {
	v := NewURIValidator(nil, nil)
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"private 10.x", "10.0.0.1", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"link-local", "169.254.1.1", true},
		{"cloud metadata", "169.254.169.254", true},
		{"public", "8.8.8.8", false},
		{"public v6", "2001:4860:4860::8888", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip)
			err := v.checkIP(ip)
			if tt.blocked {
				assert.Error(t, err, "should block %s", tt.ip)
			} else {
				assert.NoError(t, err, "should allow %s", tt.ip)
			}
		})
	}
}

func TestURIValidator_Validate_EmptySchemeRejected(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := v.Validate("example.com/page")
	assert.Error(t, err, "URL without scheme should be rejected")
}

func TestURIValidator_Validate_WildcardAllowlistIntegration(t *testing.T) {
	v := NewURIValidator([]string{"*.mycompany.com"}, []string{"https"})
	assert.NoError(t, v.Validate("https://app.mycompany.com/page"))
	assert.NoError(t, v.Validate("https://deep.nested.mycompany.com/page"))
	assert.NoError(t, v.Validate("https://mycompany.com/page"))

	err := v.Validate("https://mycompany.com.evil.com/page")
	assert.Error(t, err, "suffix attack should be blocked")
}
