package saml

import (
	"strings"
	"testing"
)

// TestFetchMetadataFromURLBlocksInternalTarget verifies the SAML IdP-metadata
// fetch (an admin-set, runtime-mutable URL) is pinned through the hardened
// client and refuses internal/private targets at dial time (SSRF). This closes
// the auth/saml/ gap that the recursive lint scan (#471) now also enforces.
func TestFetchMetadataFromURLBlocksInternalTarget(t *testing.T) {
	cases := map[string]string{
		"http://169.254.169.254/metadata": "cloud metadata",
		"http://10.0.0.1/metadata":        "private",
		"http://127.0.0.1/metadata":       "loopback",
		"http://169.254.1.1/metadata":     "link-local",
	}
	for metadataURL, want := range cases {
		_, err := fetchMetadataFromURL(metadataURL)
		if err == nil {
			t.Errorf("fetchMetadataFromURL(%s) = nil error, want %q block", metadataURL, want)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("fetchMetadataFromURL(%s) error = %q, want substring %q", metadataURL, err, want)
		}
	}
}
