// Package version holds build-time version information injected via ldflags:
//
//	-X github.com/benyamin-git/kumiho/internal/version.Version=...
package version

import "fmt"

var (
	// Version is the release version or git describe output.
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "unknown"
	// Date is the UTC build timestamp.
	Date = "unknown"
)

// String returns a human-readable version string with build metadata.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
