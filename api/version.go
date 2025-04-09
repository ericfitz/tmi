package api

import (
	"fmt"
	"strconv"
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