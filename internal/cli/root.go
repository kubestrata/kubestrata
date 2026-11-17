// Package cli wires the kubestrata commands (Cobra, styled by Fang).
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"
)

// SchemaVersion of every JSON document kubestrata prints or writes. Consumers must refuse a
// higher value than the one they support.
const SchemaVersion = 1

type globals struct {
	kubeconfig string
	format     string
}

// NewRoot builds the command tree. Kept separate from Execute so tests can run commands
// against buffers.
func NewRoot(version string) *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:   "kubestrata",
		Short: "Time-layered inventory of Kubernetes clusters for migration planning",
		Long: "kubestrata collects a read-only inventory of your kubeconfig contexts into a local cache " +
			"and renders an offline HTML view: newest resources in the center, oldest at the edge, " +
			"with a side-by-side comparison of two clusters.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&g.kubeconfig, "kubeconfig", "",
		"path to a kubeconfig file (default: KUBECONFIG rules, then ~/.kube/config)")
	root.PersistentFlags().StringVar(&g.format, "format", "text", "output format: text or json")
	root.AddCommand(newContextsCmd(g))
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute(ctx context.Context, version string) int {
	if err := fang.Execute(ctx, NewRoot(version), fang.WithVersion(version)); err != nil {
		return 1
	}
	return 0
}

func (g *globals) checkFormat() error {
	if g.format != "text" && g.format != "json" {
		return fmt.Errorf("--format must be text or json, got %q", g.format)
	}
	return nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
