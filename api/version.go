package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

func init() {
	// If GitCommit wasn't set at build time via ldflags, try environment variables.
	// Heroku sets SOURCE_VERSION to the git commit SHA during builds.
	if GitCommit == "development" {
		if commit := os.Getenv("SOURCE_VERSION"); commit != "" {
			// Use short commit hash (7 chars) like git rev-parse --short
			if len(commit) > 7 {
				GitCommit = commit[:7]
			} else {
				GitCommit = commit
			}
		}
	}
}

// Version contains versioning information for the API
type Version struct {
	Major      int    `json:"major"`
	Minor      int    `json:"minor"`
	Patch      int    `json:"patch"`
	PreRelease string `json:"pre_release,omitempty"`
	GitCommit  string `json:"git_commit,omitempty"`
	BuildDate  string `json:"build_date,omitempty"`
	APIVersion string `json:"api_version"`
}

// These values are set during build time
var (
	// Major version number
	VersionMajor = "1"
	// Minor version number
	VersionMinor = "2"
	// Patch version number
	VersionPatch = "4"
	// VersionPreRelease is the pre-release label (e.g., "rc.0", "beta.1"), empty for stable releases
	VersionPreRelease = ""
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
		PreRelease: VersionPreRelease,
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
	version := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.PreRelease != "" {
		version += "-" + v.PreRelease
	}
	return fmt.Sprintf("tmi %s (%s - built %s)", version, v.GitCommit, v.BuildDate)
}

// ApiInfoHandler handles requests to the root endpoint
type ApiInfoHandler struct {
	server *Server
}

// NewApiInfoHandler creates a new handler for API info
func NewApiInfoHandler(server *Server) *ApiInfoHandler {
	return &ApiInfoHandler{
		server: server,
	}
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

// healthCheckTimeout is the maximum time allowed for health checks
const healthCheckTimeout = 100 * time.Millisecond

// GetApiInfo returns service, API, and operator information
func (h *ApiInfoHandler) GetApiInfo(c *gin.Context) {
	// Get logger from context
	logger := slogging.GetContextLogger(c)

	logger.Debug("Handling root endpoint request from %s", c.ClientIP())
	// Log header names only to avoid exposing sensitive values
	headerNames := make([]string, 0, len(c.Request.Header))
	for k := range c.Request.Header {
		headerNames = append(headerNames, k)
	}
	logger.Debug("Request headers present: %v", headerNames)

	// Get user from context if available (will be "anonymous" for public paths)
	if userName, exists := c.Get("userName"); exists {
		if name, ok := userName.(string); ok {
			logger.Debug("Root endpoint accessed by user: %s", name)
		}
	} else {
		logger.Debug("No user found in context for root endpoint")
	}

	// Perform health checks on database and Redis
	healthChecker := NewHealthChecker(healthCheckTimeout)
	healthResult := healthChecker.CheckHealth(c.Request.Context())
	logger.Debug("Health check completed: overall=%s, db=%s, redis=%s",
		healthResult.Overall, healthResult.Database.Status, healthResult.Redis.Status)

	// Get version info
	v := GetVersion()
	version := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	var buildString string
	if v.PreRelease != "" {
		// Semver: pre-release with dash, build metadata with plus
		buildString = fmt.Sprintf("%s-%s+%s", version, v.PreRelease, v.GitCommit)
	} else {
		// Stable release: keep existing format for backward compatibility
		buildString = fmt.Sprintf("%s-%s", version, v.GitCommit)
	}

	// Get API version from embedded OpenAPI specification
	apiVersion := string(ComponentHealthStatusUnknown)
	swagger, err := GetSwagger()
	if err != nil {
		logger.Error("Failed to load OpenAPI spec: %v", err)
	} else if swagger != nil && swagger.Info != nil {
		apiVersion = swagger.Info.Version
		logger.Debug("Loaded API version from OpenAPI spec: %s", apiVersion)
	}

	// Create ApiInfo response with health-aware status
	apiInfo := ApiInfo{
		Status: struct {
			Code ApiInfoStatusCode `json:"code"`
			Time time.Time         `json:"time"`
		}{
			Code: healthResult.Overall,
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
			Version:       apiVersion,
			Specification: "https://github.com/ericfitz/tmi/blob/main/api-schema/tmi-openapi.json",
		},
	}

	// Include health details only when status is DEGRADED
	if healthResult.Overall == DEGRADED {
		apiInfo.Health = &struct {
			Database *ComponentHealth `json:"database,omitempty"`
			Redis    *ComponentHealth `json:"redis,omitempty"`
		}{
			Database: healthResult.Database.ToAPIComponentHealth(),
			Redis:    healthResult.Redis.ToAPIComponentHealth(),
		}
		logger.Warn("System health is degraded: database=%s, redis=%s",
			healthResult.Database.Status, healthResult.Redis.Status)
	}

	// Add optional operator info from config (stored in context)
	operatorName, _ := c.Get("operatorName")
	operatorContact, _ := c.Get("operatorContact")

	// Convert to strings safely
	nameStr, nameOk := operatorName.(string)
	contactStr, contactOk := operatorContact.(string)

	if (nameOk && nameStr != "") || (contactOk && contactStr != "") {
		apiInfo.Operator = &struct {
			Contact string `json:"contact"`
			Name    string `json:"name"`
		}{
			Name:    nameStr,
			Contact: contactStr,
		}
		logger.Debug("Added operator info: name=%s, contact=%s", nameStr, contactStr)
	}

	// Note: WebSocket information is now documented in AsyncAPI specification
	// WebSocket endpoints are not part of REST API info since they use different protocols

	logger.Info("Returning API info response")

	// Check if request is from a browser
	acceptHeader := c.GetHeader("Accept")
	if strings.Contains(acceptHeader, "text/html") {
		// Return HTML page for browser requests
		apiInfoJSON, err := json.Marshal(apiInfo)
		if err != nil {
			logger.Error("Failed to marshal API info: %v", err)
			c.JSON(http.StatusInternalServerError, Error{
				Error:            "server_error",
				ErrorDescription: "Failed to generate API info",
			})
			return
		}

		// Format HTML with API info as JSON - escape for JavaScript context to prevent XSS
		escapedJSON := strings.ReplaceAll(string(apiInfoJSON), "</", "<\\/")
		escapedJSON = strings.ReplaceAll(escapedJSON, "<!--", "<\\!--")
		htmlResponse := fmt.Sprintf(rootPageHTML, escapedJSON)
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, htmlResponse)
		logger.Debug("Returned HTML response for browser")
	} else {
		// Return JSON for API clients
		c.JSON(http.StatusOK, apiInfo)
		logger.Debug("Returned JSON response for API client")
	}
}
