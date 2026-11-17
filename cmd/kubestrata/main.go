// Command kubestrata renders a time-layered inventory of Kubernetes clusters.
package main

import (
	"context"
	"os"
	"runtime/debug"

	"github.com/leosavio/kubestrata/internal/cli"
)

// version is set by the release build (-ldflags "-X main.version=vX.Y.Z").
var version = ""

func main() {
	os.Exit(cli.Execute(context.Background(), resolveVersion()))
}

// resolveVersion prefers the ldflags value, then the module version from `go install`.
func resolveVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}
