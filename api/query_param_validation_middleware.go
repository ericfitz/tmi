package api

import (
	"net/http"
	"net/url"
	"unicode/utf8"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/unicodecheck"
	"github.com/gin-gonic/gin"
)

// QueryParameterControlCharMiddleware is the single enforcement point that
// rejects control characters (NUL and the rest of C0/C1, excluding tab, CR and
// LF) in any query parameter name or value with a 400 before handlers run.
//
// Request bodies already get this policy from UnicodeNormalizationMiddleware;
// query strings did not, so "?name=a%00b" reached PostgreSQL, which rejects
// 0x00 in text (SQLSTATE 22021) and surfaced as a 500. The check is
// database-independent: Oracle may accept NUL, but a control character is
// never legitimate filter input, so every backend behaves the same.
//
// Invalid UTF-8 is rejected too (PostgreSQL also fails it with SQLSTATE 22021).
// Pairs that do not parse (e.g. a malformed percent escape) are skipped by
// ParseQuery and left to the normal request validation. The offending parameter value is never
// logged.
func QueryParameterControlCharMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.RawQuery == "" {
			c.Next()
			return
		}
		// ParseQuery returns the pairs it could parse even when it also reports
		// an error (malformed escape, ';'), and Gin's c.Query reads that same
		// partial map. Scan it regardless, or "?x=%zz&name=a%00b" bypasses the
		// check. A malformed but otherwise clean query still passes through.
		params, _ := url.ParseQuery(c.Request.URL.RawQuery)
		for name, values := range params {
			bad := unsafeQueryText(name)
			for _, v := range values {
				if unsafeQueryText(v) {
					bad = true
				}
			}
			if bad {
				slogging.Get().WithContext(c).Warn("Query parameter contains control characters")
				c.JSON(http.StatusBadRequest, Error{
					Error:            ErrorError(unicodeErrorCode(c)),
					ErrorDescription: "Query parameters must not contain control characters",
				})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// unsafeQueryText reports whether a decoded query name or value contains a
// control character or is not valid UTF-8.
func unsafeQueryText(s string) bool {
	return !utf8.ValidString(s) || unicodecheck.ContainsControlChars(s)
}
