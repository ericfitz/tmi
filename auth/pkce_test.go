package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RFC 7636 Appendix B test vectors
const (
	rfcTestVerifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	rfcTestChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
)

// TestGenerateCodeVerifier tests PKCE code verifier generation.
func TestGenerateCodeVerifier(t *testing.T) {
	t.Run("produces_valid_format", func(t *testing.T) {
		verifier, err := GenerateCodeVerifier()
		require.NoError(t, err)

		// 32 random bytes → 43 base64url chars (no padding)
		assert.Equal(t, 43, len(verifier),
			"Verifier should be 43 characters (32 bytes base64url-encoded)")

		err = ValidateCodeVerifierFormat(verifier)
		assert.NoError(t, err, "Generated verifier should pass format validation")
	})

	t.Run("produces_unique_values", func(t *testing.T) {
		v1, err := GenerateCodeVerifier()
		require.NoError(t, err)
		v2, err := GenerateCodeVerifier()
		require.NoError(t, err)

		assert.NotEqual(t, v1, v2,
			"Two generated verifiers should be different (crypto/rand)")
	})

	t.Run("uses_base64url_charset", func(t *testing.T) {
		verifier, err := GenerateCodeVerifier()
		require.NoError(t, err)

		assert.True(t, verifierRegex.MatchString(verifier),
			"Verifier should only contain base64url-safe characters")
		// base64url uses - and _ instead of + and /
		assert.NotContains(t, verifier, "+")
		assert.NotContains(t, verifier, "/")
		assert.NotContains(t, verifier, "=")
	})
}

// TestComputeS256Challenge tests the SHA-256 challenge computation.
func TestComputeS256Challenge(t *testing.T) {
	t.Run("known_rfc_vector", func(t *testing.T) {
		// RFC 7636 Appendix B example
		verifier := rfcTestVerifier
		expected := rfcTestChallenge

		challenge := ComputeS256Challenge(verifier)
		assert.Equal(t, expected, challenge)
	})

	t.Run("deterministic", func(t *testing.T) {
		verifier := "test-verifier-with-valid-length-that-meets-requirements"
		c1 := ComputeS256Challenge(verifier)
		c2 := ComputeS256Challenge(verifier)
		assert.Equal(t, c1, c2, "Same verifier should produce same challenge")
	})

	t.Run("different_verifiers_produce_different_challenges", func(t *testing.T) {
		c1 := ComputeS256Challenge("verifier-one-with-sufficient-length-padding-chars")
		c2 := ComputeS256Challenge("verifier-two-with-sufficient-length-padding-chars")
		assert.NotEqual(t, c1, c2)
	})

	t.Run("output_is_base64url_no_padding", func(t *testing.T) {
		challenge := ComputeS256Challenge("test-input")
		// SHA-256 → 32 bytes → 43 base64url chars (no padding)
		assert.Equal(t, 43, len(challenge))
		assert.NotContains(t, challenge, "=")
		assert.NotContains(t, challenge, "+")
		assert.NotContains(t, challenge, "/")
	})
}

// TestValidateCodeChallenge tests PKCE challenge verification with constant-time comparison.
func TestValidateCodeChallenge(t *testing.T) {
	t.Run("valid_s256_challenge", func(t *testing.T) {
		verifier, err := GenerateCodeVerifier()
		require.NoError(t, err)

		challenge := ComputeS256Challenge(verifier)

		err = ValidateCodeChallenge(verifier, challenge, "S256")
		assert.NoError(t, err)
	})

	t.Run("wrong_verifier_rejected", func(t *testing.T) {
		verifier, err := GenerateCodeVerifier()
		require.NoError(t, err)

		challenge := ComputeS256Challenge(verifier)

		wrongVerifier, err := GenerateCodeVerifier()
		require.NoError(t, err)

		err = ValidateCodeChallenge(wrongVerifier, challenge, "S256")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})

	t.Run("wrong_challenge_rejected", func(t *testing.T) {
		verifier, err := GenerateCodeVerifier()
		require.NoError(t, err)

		wrongChallenge := ComputeS256Challenge("wrong-verifier-value-with-correct-format")

		err = ValidateCodeChallenge(verifier, wrongChallenge, "S256")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})

	t.Run("plain_method_rejected", func(t *testing.T) {
		// RFC 7636 Section 4.2: "plain" is less secure.
		// TMI only supports S256.
		err := ValidateCodeChallenge("verifier", "challenge", "plain")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported code_challenge_method")
	})

	t.Run("empty_method_rejected", func(t *testing.T) {
		err := ValidateCodeChallenge("verifier", "challenge", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported")
	})

	t.Run("case_sensitive_method", func(t *testing.T) {
		// "s256" (lowercase) should be rejected — only "S256" is accepted
		err := ValidateCodeChallenge("verifier", "challenge", "s256")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported")
	})

	t.Run("rfc_appendix_b_example", func(t *testing.T) {
		verifier := rfcTestVerifier
		challenge := rfcTestChallenge

		err := ValidateCodeChallenge(verifier, challenge, "S256")
		assert.NoError(t, err, "RFC 7636 Appendix B example should validate")
	})
}

// TestValidateCodeVerifierFormat tests code verifier format validation per RFC 7636.
func TestValidateCodeVerifierFormat(t *testing.T) {
	t.Run("valid_43_char_verifier", func(t *testing.T) {
		// Exactly MinVerifierLength characters
		verifier := strings.Repeat("a", MinVerifierLength)
		err := ValidateCodeVerifierFormat(verifier)
		assert.NoError(t, err)
	})

	t.Run("valid_128_char_verifier", func(t *testing.T) {
		// Exactly MaxVerifierLength characters
		verifier := strings.Repeat("a", MaxVerifierLength)
		err := ValidateCodeVerifierFormat(verifier)
		assert.NoError(t, err)
	})

	t.Run("empty_verifier_rejected", func(t *testing.T) {
		err := ValidateCodeVerifierFormat("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("too_short_42_chars_rejected", func(t *testing.T) {
		verifier := strings.Repeat("a", MinVerifierLength-1)
		err := ValidateCodeVerifierFormat(verifier)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "length must be between")
	})

	t.Run("too_long_129_chars_rejected", func(t *testing.T) {
		verifier := strings.Repeat("a", MaxVerifierLength+1)
		err := ValidateCodeVerifierFormat(verifier)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "length must be between")
	})

	t.Run("allowed_special_chars", func(t *testing.T) {
		// RFC 7636: [A-Z] / [a-z] / [0-9] / "-" / "." / "_" / "~"
		verifier := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij-._~01234"
		err := ValidateCodeVerifierFormat(verifier)
		assert.NoError(t, err)
	})

	t.Run("space_rejected", func(t *testing.T) {
		verifier := strings.Repeat("a", 42) + " "
		err := ValidateCodeVerifierFormat(verifier)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid characters")
	})

	t.Run("plus_sign_rejected", func(t *testing.T) {
		// + is allowed in standard base64 but NOT in code verifiers
		verifier := strings.Repeat("a", 42) + "+"
		err := ValidateCodeVerifierFormat(verifier)
		assert.Error(t, err)
	})

	t.Run("slash_rejected", func(t *testing.T) {
		verifier := strings.Repeat("a", 42) + "/"
		err := ValidateCodeVerifierFormat(verifier)
		assert.Error(t, err)
	})

	t.Run("equals_sign_rejected", func(t *testing.T) {
		verifier := strings.Repeat("a", 42) + "="
		err := ValidateCodeVerifierFormat(verifier)
		assert.Error(t, err)
	})
}

// TestValidateCodeChallengeFormat tests code challenge format validation.
func TestValidateCodeChallengeFormat(t *testing.T) {
	t.Run("valid_challenge", func(t *testing.T) {
		// Generate a valid challenge from a verifier
		verifier, err := GenerateCodeVerifier()
		require.NoError(t, err)
		challenge := ComputeS256Challenge(verifier)

		err = ValidateCodeChallengeFormat(challenge)
		assert.NoError(t, err)
	})

	t.Run("empty_challenge_rejected", func(t *testing.T) {
		err := ValidateCodeChallengeFormat("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("too_short_rejected", func(t *testing.T) {
		challenge := strings.Repeat("a", MinVerifierLength-1)
		err := ValidateCodeChallengeFormat(challenge)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "length must be between")
	})

	t.Run("too_long_rejected", func(t *testing.T) {
		challenge := strings.Repeat("a", MaxVerifierLength+1)
		err := ValidateCodeChallengeFormat(challenge)
		assert.Error(t, err)
	})

	t.Run("base64url_chars_accepted", func(t *testing.T) {
		// Challenge format is base64url: [A-Za-z0-9-_] (no padding, no + or /)
		challenge := strings.Repeat("A", 43)
		err := ValidateCodeChallengeFormat(challenge)
		assert.NoError(t, err)
	})

	t.Run("padding_equals_rejected", func(t *testing.T) {
		// base64url does not use padding
		challenge := strings.Repeat("a", 42) + "="
		err := ValidateCodeChallengeFormat(challenge)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid characters")
	})

	t.Run("dot_rejected_in_challenge", func(t *testing.T) {
		// Dot is allowed in verifiers but NOT in challenges (not in base64url)
		challenge := strings.Repeat("a", 42) + "."
		err := ValidateCodeChallengeFormat(challenge)
		assert.Error(t, err)
	})

	t.Run("tilde_rejected_in_challenge", func(t *testing.T) {
		// Tilde is allowed in verifiers but NOT in challenges (not in base64url)
		challenge := strings.Repeat("a", 42) + "~"
		err := ValidateCodeChallengeFormat(challenge)
		assert.Error(t, err)
	})
}

// TestPKCEEndToEnd tests a complete PKCE flow: generate → compute challenge → validate.
func TestPKCEEndToEnd(t *testing.T) {
	t.Run("complete_flow", func(t *testing.T) {
		// Step 1: Client generates verifier
		verifier, err := GenerateCodeVerifier()
		require.NoError(t, err)

		// Step 2: Client computes challenge
		challenge := ComputeS256Challenge(verifier)

		// Step 3: Validate formats
		err = ValidateCodeVerifierFormat(verifier)
		require.NoError(t, err)
		err = ValidateCodeChallengeFormat(challenge)
		require.NoError(t, err)

		// Step 4: Server validates verifier against stored challenge
		err = ValidateCodeChallenge(verifier, challenge, "S256")
		assert.NoError(t, err)
	})

	t.Run("replay_with_different_verifier_fails", func(t *testing.T) {
		verifier1, err := GenerateCodeVerifier()
		require.NoError(t, err)
		challenge := ComputeS256Challenge(verifier1)

		// Attacker tries to use a different verifier
		verifier2, err := GenerateCodeVerifier()
		require.NoError(t, err)

		err = ValidateCodeChallenge(verifier2, challenge, "S256")
		assert.Error(t, err, "Different verifier should not match challenge")
	})
}
