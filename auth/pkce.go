package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"regexp"
)

// PKCE constants per RFC 7636
const (
	// MinVerifierLength is the minimum length for a code verifier (43 characters)
	MinVerifierLength = 43
	// MaxVerifierLength is the maximum length for a code verifier (128 characters)
	MaxVerifierLength = 128
	// VerifierByteLength is the number of random bytes to generate (32 bytes = 43 base64url chars)
	VerifierByteLength = 32
)

var (
	// verifierRegex validates code_verifier format: [A-Z] / [a-z] / [0-9] / "-" / "." / "_" / "~"
	verifierRegex = regexp.MustCompile(`^[A-Za-z0-9\-._~]+$`)
	// challengeRegex validates code_challenge format: base64url encoded
	challengeRegex = regexp.MustCompile(`^[A-Za-z0-9\-_]+$`)
)

// GenerateCodeVerifier generates a cryptographically secure random code verifier
// Returns a 43-character base64url-encoded string (32 random bytes)
func GenerateCodeVerifier() (string, error) {
	// Generate 32 random bytes
	verifierBytes := make([]byte, VerifierByteLength)
	_, err := rand.Read(verifierBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %v", err)
	}

	// Encode as base64url without padding
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	return verifier, nil
}

// ComputeS256Challenge computes the S256 code challenge from a code verifier
// Returns base64url(SHA256(codeVerifier))
func ComputeS256Challenge(codeVerifier string) string {
	hash := sha256.Sum256([]byte(codeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	return challenge
}

// ValidateCodeChallenge validates that a code verifier matches the code challenge
// Uses constant-time comparison to prevent timing attacks
func ValidateCodeChallenge(codeVerifier, codeChallenge, method string) error {
	// Only S256 method is supported
	if method != "S256" {
		return fmt.Errorf("unsupported code_challenge_method: %s (only S256 is supported)", method)
	}

	// Compute expected challenge from verifier
	expectedChallenge := ComputeS256Challenge(codeVerifier)

	// Use constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare([]byte(expectedChallenge), []byte(codeChallenge)) != 1 {
		return fmt.Errorf("code_verifier does not match code_challenge")
	}

	return nil
}

// ValidateCodeChallengeFormat validates the format of a code challenge
func ValidateCodeChallengeFormat(challenge string) error {
	if challenge == "" {
		return fmt.Errorf("code_challenge cannot be empty")
	}

	if len(challenge) < MinVerifierLength || len(challenge) > MaxVerifierLength {
		return fmt.Errorf("code_challenge length must be between %d and %d characters", MinVerifierLength, MaxVerifierLength)
	}

	if !challengeRegex.MatchString(challenge) {
		return fmt.Errorf("code_challenge contains invalid characters (must be base64url encoded)")
	}

	return nil
}

// ValidateCodeVerifierFormat validates the format of a code verifier
func ValidateCodeVerifierFormat(verifier string) error {
	if verifier == "" {
		return fmt.Errorf("code_verifier cannot be empty")
	}

	if len(verifier) < MinVerifierLength || len(verifier) > MaxVerifierLength {
		return fmt.Errorf("code_verifier length must be between %d and %d characters", MinVerifierLength, MaxVerifierLength)
	}

	if !verifierRegex.MatchString(verifier) {
		return fmt.Errorf("code_verifier contains invalid characters (allowed: [A-Za-z0-9-._~])")
	}

	return nil
}
