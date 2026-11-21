package api

import (
	"net/http"
	"net/url"

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
// A query string that does not parse (e.g. a malformed percent escape) is left
// to the normal request validation. The offending parameter value is never
// logged.
func QueryParameterControlCharMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.RawQuery == "" {
			c.Next()
			return
		}
		params, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil {
			c.Next()
			return
		}
		for name, values := range params {
			bad := unicodecheck.ContainsControlChars(name)
			for _, v := range values {
				if unicodecheck.ContainsControlChars(v) {
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
