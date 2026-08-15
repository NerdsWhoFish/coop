// Package version carries build metadata stamped in by the linker.
package version

import (
	"fmt"
	"runtime"
)

// Overridden at build time via -ldflags. See the Makefile.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String renders a one-line build identifier.
func String() string {
	return fmt.Sprintf("coop %s (commit %s, built %s, %s/%s, %s)",
		Version, Commit, Date, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
