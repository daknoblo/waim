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
	// Version is the build version, formatted as vYYYYMMDD-HHMM at build time.
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
// plain semver, every other build carries a date stamp or "dev".
func (i Info) IsRelease() bool {
	return semver.MatchString(i.Version)
}

// IsFeatureRelease reports whether a GitHub Release page exists for this build.
// The release workflow only opens one for X.Y.0; patch tags publish an image
// and nothing else.
func (i Info) IsFeatureRelease() bool {
	return i.IsRelease() && strings.HasSuffix(i.Version, ".0")
}
