package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
)

// Version contains versioning information for the API
type Version struct {
	Major      int    `json:"major"`
	Minor      int    `json:"minor"`
	Patch      int    `json:"patch"`
	GitCommit  string `json:"git_commit,omitempty"`
	BuildDate  string `json:"build_date,omitempty"`
	APIVersion string `json:"api_version"`
}

// These values are set during build time
var (
	// Major version number
	VersionMajor = "0"
	// Minor version number
	VersionMinor = "1"
	// Patch version number
	VersionPatch = "0"
	// GitCommit is the git commit hash from build
	GitCommit = "development"
	// BuildDate is the build timestamp
	BuildDate = "unknown"
	// APIVersion is the API version string
	APIVersion = "v1"
)

// GetVersion returns the current application version
func GetVersion() Version {
	major := parseIntOrZero(VersionMajor)
	minor := parseIntOrZero(VersionMinor)
	patch := parseIntOrZero(VersionPatch)

	return Version{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		GitCommit:  GitCommit,
		BuildDate:  BuildDate,
		APIVersion: APIVersion,
	}
}

// parseIntOrZero parses an integer from a string, returning 0 on failure
func parseIntOrZero(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return i
}

// GetVersionString returns the version as a formatted string
func GetVersionString() string {
	v := GetVersion()
	return fmt.Sprintf("tmi %d.%d.%d (%s - built %s)",
		v.Major, v.Minor, v.Patch, v.GitCommit, v.BuildDate)
}

// ApiInfoHandler handles requests to the root endpoint
type ApiInfoHandler struct {
	// Add dependencies if needed
}

// NewApiInfoHandler creates a new handler for API info
func NewApiInfoHandler() *ApiInfoHandler {
	return &ApiInfoHandler{}
}

// HTML template for root page when accessed from browser
const rootPageHTML = `<!DOCTYPE html>
<html>
<head>
    <title>TMI API Server</title>
		<link rel="icon" type="image/png" href="/favicon-96x96.png" sizes="96x96" />
		<link rel="icon" type="image/svg+xml" href="/favicon.svg" />
		<link rel="shortcut icon" href="/favicon.ico" />
		<link rel="apple-touch-icon" sizes="180x180" href="/apple-touch-icon.png" />
		<link rel="manifest" href="/site.webmanifest" />
	    <style>
			body { font-family: Arial, sans-serif; margin: 40px; line-height: 1.6; }
			.container { max-width: 800px; margin: 0 auto; }
			h1 { color: #333; }
			pre { background: #f5f5f5; padding: 10px; border-radius: 5px; overflow-x: auto; }
		</style>
</head>
<body>
    <div class="container">
        <h1>TMI API Server</h1>
        <p>This is the API server for the Threat Modeling Improved (TMI) application.</p>
        <p>API information:</p>
        <pre id="api-info"></pre>
    </div>
    <script>
        // Format and display the API info
        const apiInfo = %s;
        document.getElementById('api-info').textContent = JSON.stringify(apiInfo, null, 2);
    </script>
</body>
</html>
`

// GetApiInfo returns service, API, and operator information
func (h *ApiInfoHandler) GetApiInfo(c *gin.Context) {
	// Get logger from context
	logger := logging.GetContextLogger(c)

	logger.Debug("Handling root endpoint request from %s", c.ClientIP())
	logger.Debug("Request headers: %v", c.Request.Header)

	// Get user from context if available (will be "anonymous" for public paths)
	if userName, exists := c.Get("userName"); exists {
		if name, ok := userName.(string); ok {
			logger.Debug("Root endpoint accessed by user: %s", name)
		}
	} else {
		logger.Debug("No user found in context for root endpoint")
	}

	// Get version info
	v := GetVersion()
	buildString := fmt.Sprintf("%d.%d.%d-%s", v.Major, v.Minor, v.Patch, v.GitCommit)

	// Create ApiInfo response
	apiInfo := ApiInfo{
		Status: struct {
			Code ApiInfoStatusCode `json:"code"`
			Time time.Time         `json:"time"`
		}{
			Code: OK,
			Time: time.Now().UTC(),
		},
		Service: struct {
			Build string `json:"build"`
			Name  string `json:"name"`
		}{
			Name:  "TMI",
			Build: buildString,
		},
		Api: struct {
			Specification string `json:"specification"`
			Version       string `json:"version"`
		}{
			Version:       "1.0.0",
			Specification: "https://github.com/ericfitz/tmi/blob/main/tmi-openapi.json",
		},
	}

	// Add optional operator info if environment variables are set
	operatorName := os.Getenv("TMI_OPERATOR_NAME")
	operatorContact := os.Getenv("TMI_OPERATOR_CONTACT")
	if operatorName != "" || operatorContact != "" {
		apiInfo.Operator = &struct {
			Contact string `json:"contact"`
			Name    string `json:"name"`
		}{
			Name:    operatorName,
			Contact: operatorContact,
		}
		logger.Debug("Added operator info: name=%s, contact=%s", operatorName, operatorContact)
	}

	logger.Info("Returning API info response")

	// Check if request is from a browser
	acceptHeader := c.GetHeader("Accept")
	if strings.Contains(acceptHeader, "text/html") {
		// Return HTML page for browser requests
		apiInfoJSON, err := json.Marshal(apiInfo)
		if err != nil {
			logger.Error("Failed to marshal API info: %v", err)
			c.JSON(http.StatusInternalServerError, Error{
				Error:   "server_error",
				Message: "Failed to generate API info",
			})
			return
		}

		// Format HTML with API info as JSON
		html := fmt.Sprintf(rootPageHTML, string(apiInfoJSON))
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, html)
		logger.Debug("Returned HTML response for browser")
	} else {
		// Return JSON for API clients
		c.JSON(http.StatusOK, apiInfo)
		logger.Debug("Returned JSON response for API client")
	}
}
