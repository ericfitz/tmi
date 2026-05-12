package api

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// validJPEGDataURL is a small valid data URL whose base64 body is the bytes
// "hello world" — not a real JPEG, but the validator only checks the data-URL
// envelope (MIME prefix + valid base64 + length), not image content.
func validJPEGDataURL() string {
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("hello world"))
}

func TestValidateScreenshot_NilIsValid(t *testing.T) {
	assert.NoError(t, validateScreenshot(nil))
}

func TestValidateScreenshot_AcceptsValidJPEG(t *testing.T) {
	s := validJPEGDataURL()
	assert.NoError(t, validateScreenshot(&s))
}

func TestValidateScreenshot_AcceptsValidPNG(t *testing.T) {
	s := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4E, 0x47})
	assert.NoError(t, validateScreenshot(&s))
}

func TestValidateScreenshot_AcceptsValidWebP(t *testing.T) {
	s := "data:image/webp;base64," + base64.StdEncoding.EncodeToString([]byte("RIFF"))
	assert.NoError(t, validateScreenshot(&s))
}

func TestValidateScreenshot_RejectsWrongMIME(t *testing.T) {
	cases := []string{
		"data:image/gif;base64,AAAA",
		"data:text/plain;base64,AAAA",
		"data:application/pdf;base64,AAAA",
		"data:;base64,AAAA",
		"http://example.com/image.jpg",
		"AAAA",
		"",
	}
	for _, s := range cases {
		s := s
		t.Run(s, func(t *testing.T) {
			err := validateScreenshot(&s)
			require := assert.Error
			require(t, err)
			var re *RequestError
			if !errors.As(err, &re) {
				t.Fatalf("expected *RequestError, got %T", err)
			}
			assert.Equal(t, 400, re.Status)
		})
	}
}

func TestValidateScreenshot_RejectsBadBase64(t *testing.T) {
	s := "data:image/jpeg;base64,not_base64_$$$"
	err := validateScreenshot(&s)
	if assert.Error(t, err) {
		var re *RequestError
		if !errors.As(err, &re) {
			t.Fatalf("expected *RequestError, got %T", err)
		}
		assert.Equal(t, 400, re.Status)
	}
}

func TestValidateScreenshot_RejectsEmptyBody(t *testing.T) {
	s := "data:image/jpeg;base64,"
	err := validateScreenshot(&s)
	if assert.Error(t, err) {
		var re *RequestError
		if !errors.As(err, &re) {
			t.Fatalf("expected *RequestError, got %T", err)
		}
		assert.Equal(t, 400, re.Status)
	}
}

func TestValidateScreenshot_RejectsOversize(t *testing.T) {
	// Construct a data URL just over the cap. We don't need the base64 body to
	// be valid — the length check fires first.
	body := strings.Repeat("A", maxScreenshotBytes+1)
	s := "data:image/jpeg;base64," + body
	err := validateScreenshot(&s)
	if assert.Error(t, err) {
		var re *RequestError
		if !errors.As(err, &re) {
			t.Fatalf("expected *RequestError, got %T", err)
		}
		assert.Equal(t, 413, re.Status)
	}
}
