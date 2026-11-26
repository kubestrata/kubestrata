// Package cli wires the kubestrata commands (Cobra, styled by Fang).
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"

	"github.com/kubestrata/kubestrata/internal/redact"
	"github.com/kubestrata/kubestrata/internal/snapshot"
)

// SchemaVersion of every JSON document kubestrata prints or writes. Consumers must refuse a
// higher value than the one they support.
const SchemaVersion = 1

type globals struct {
	kubeconfig string
	format     string
	cacheDir   string
	version    string
	redact     bool
}

// NewRoot builds the command tree. Kept separate from Execute so tests can run commands
// against buffers.
func NewRoot(version string) *cobra.Command {
	g := &globals{version: version}
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
	root.PersistentFlags().StringVar(&g.cacheDir, "cache-dir", "",
		"snapshot cache (default: $KUBESTRATA_CACHE_DIR or the user cache dir)")
	root.PersistentFlags().BoolVar(&g.redact, "redact", false,
		"mask AWS account ids and EKS/AKS endpoint hosts in the output, --snapshot-out and the site (the cache keeps the real values)")
	root.AddCommand(newContextsCmd(g), newCollectCmd(g), newCacheCmd(g), newRenderCmd(g), newCompareCmd(g), newDiffCmd(g))
	g.maskErrors(root)
	return root
}

// maskErrors wraps the Args and RunE of c and of every subcommand so that, under --redact, the
// error Fang prints goes through the same mask as the output: messages name contexts, and EKS
// contexts are often ARNs that carry the account id.
func (g *globals) maskErrors(c *cobra.Command) {
	if args := c.Args; args != nil {
		c.Args = func(cmd *cobra.Command, a []string) error { return g.maskErr(args(cmd, a)) }
	}
	if run := c.RunE; run != nil {
		c.RunE = func(cmd *cobra.Command, a []string) error { return g.maskErr(run(cmd, a)) }
	}
	for _, sub := range c.Commands() {
		g.maskErrors(sub)
	}
}

// maskErr returns err with its text masked under --redact. An error with nothing to mask is
// returned as is, so its chain survives.
func (g *globals) maskErr(err error) error {
	if err == nil || !g.redact {
		return err
	}
	if m := redact.Name(err.Error()); m != err.Error() {
		return errors.New(m)
	}
	return err
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

// mask redacts a name (context, cluster, user) or a message under --redact.
func (g *globals) mask(s string) string {
	if g.redact {
		return redact.Name(s)
	}
	return s
}

// snapshots returns the snapshots to render: redacted copies under --redact, else the same ones.
func (g *globals) snapshots(in []*snapshot.Snapshot) ([]*snapshot.Snapshot, error) {
	if !g.redact {
		return in, nil
	}
	out := make([]*snapshot.Snapshot, len(in))
	for i, s := range in {
		c, err := redact.Copy(s)
		if err != nil {
			return nil, err
		}
		c.Cluster.Context = redact.Name(c.Cluster.Context)
		out[i] = c
	}
	return out, nil
}
