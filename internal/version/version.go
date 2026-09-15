// Package version exposes build-time metadata injected via -ldflags.
package version

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// These values are overridden at build time using -ldflags, e.g.:
//
//	-X github.com/daknoblo/waim/internal/version.Version=1.2.3
var (
	// Version is a stable semver or a channel-prefixed build timestamp.
	Version = "dev"
	// Commit is the git commit hash.
	Commit = "unknown"
	// Date is the build date in RFC3339 format.
	Date = "unknown"
)

// Info bundles the build metadata for display in the UI and logs.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	GoVer   string `json:"goVersion"`
}

// Get returns the current build information.
func Get() Info {
	goVer := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		goVer = bi.GoVersion
	}
	return Info{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
		GoVer:   goVer,
	}
}

// String returns a compact human-readable build string.
func (i Info) String() string {
	return i.Version + " (" + i.Commit + ", built " + i.Date + ")"
}

var semver = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// IsRelease reports whether the build came from a release tag: those carry
// plain semver, every other build carries a date stamp, channel stamp or "dev".
func (i Info) IsRelease() bool {
	return semver.MatchString(i.Version)
}

// IsDevelopment identifies the explicit dev channel and local unversioned builds.
// Stable timestamps, version tags and static demos must not acquire a dev label.
func (i Info) IsDevelopment() bool {
	return i.Version == "dev" || strings.HasPrefix(i.Version, "dev-")
}

// IsFeatureRelease classifies X.Y.0 versions. Patch versions also have release
// pages; use IsRelease when deciding whether to link to release notes.
func (i Info) IsFeatureRelease() bool {
	return i.IsRelease() && strings.HasSuffix(i.Version, ".0")
}
