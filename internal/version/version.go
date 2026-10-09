// Package version holds the release version. tagpr updates it.
package version

// Version is the current release.
const Version = "0.2.0"

// Source is "release" in the binaries the release workflow builds (its
// -ldflags set it), and empty in any other build: make install, go install.
var Source string

// String is the version as syno --version prints it, marking builds that
// did not come from the release workflow.
func String() string {
	if Source == "release" {
		return Version
	}
	return Version + " (dev)"
}
