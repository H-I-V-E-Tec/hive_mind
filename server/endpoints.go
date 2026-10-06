package server

import (
	"os"
	"strings"
)

// DefaultCenterURL is the HIVE Center used when nothing else is configured.
// It is a var so a release can replace it with -ldflags -X.
var DefaultCenterURL = "https://hive-center.duckdns.org"

// mindPathOnCenter is where the Center's proxy serves the Mind API.
const mindPathOnCenter = "/mind"

// ResolveCenterURL picks the Center URL: --center-url, HIVE_CENTER_URL, the
// URL saved by the last login, then DefaultCenterURL.
func ResolveCenterURL(args []string) string {
	if value := strings.TrimSpace(flagValueFromArgs(args, "--center-url")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("HIVE_CENTER_URL")); value != "" {
		return value
	}
	if value := LoadStoredCenterURL(); value != "" {
		return value
	}
	return DefaultCenterURL
}

// ResolveMindURL picks the Mind API URL: --mind-url, HIVE_MIND_URL, then the
// Center URL plus /mind.
func ResolveMindURL(args []string) string {
	if value := strings.TrimSpace(flagValueFromArgs(args, "--mind-url")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("HIVE_MIND_URL")); value != "" {
		return value
	}
	return strings.TrimSuffix(ResolveCenterURL(args), "/") + mindPathOnCenter
}

// hasLocalConfig reports whether this process is configured as a local
// writer/reader (server or legacy install) rather than a remote member client.
func hasLocalConfig(args []string) bool {
	if strings.TrimSpace(os.Getenv("HIVE_ID")) != "" || strings.TrimSpace(os.Getenv("QDRANT_URL")) != "" {
		return true
	}
	return flagValueFromArgs(args, "--config") != ""
}
