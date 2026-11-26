// Command kubestrata renders a time-layered inventory of Kubernetes clusters.
package main

import (
	"context"
	"os"
	"runtime/debug"
	"strings"

	"github.com/kubestrata/kubestrata/internal/cli"
)

// version is set by the release build (-ldflags "-X main.version=X.Y.Z", without the v).
var version = ""

func main() {
	os.Exit(cli.Execute(context.Background(), resolveVersion()))
}

// resolveVersion prefers the ldflags value, then the module version from `go install`, printed
// without the leading v so that every install path reports the same string.
func resolveVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return "dev"
}
