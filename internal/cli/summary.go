package cli

import (
	"github.com/spf13/cobra"

	"github.com/kubestrata/kubestrata/internal/snapshot"
	"github.com/kubestrata/kubestrata/internal/summary"
)

func newSummaryCmd(g *globals) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "summary -c <context>",
		Short: "Migration summary of the newest cached snapshot of a context (no cluster access)",
		Long: "summary reads the newest cached snapshot of a context and prints, as Markdown (or one JSON document " +
			"with --format json), what a migration plan starts from: resources by category and kind, nodes by instance " +
			"type and zone, managers, the biggest namespaces, end-of-life kinds, objects without an expected owner, team " +
			"coverage and Argo CD. System namespaces are counted apart. Run `collect` first.",
		Example: "  kubestrata summary -c source > source-summary.md\n" +
			"  kubestrata summary -c source --format json --redact",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := g.checkFormat(); err != nil {
				return err
			}
			c, err := g.openCache()
			if err != nil {
				return err
			}
			if name == "" {
				targets, err := g.resolveContexts(nil, false)
				if err != nil {
					return err
				}
				name = targets[0]
			}
			s, err := loadLatest(c, name)
			if err != nil {
				return err
			}
			snaps, err := g.snapshots([]*snapshot.Snapshot{s})
			if err != nil {
				return err
			}
			sum := summary.Of(snaps[0])
			if g.format == "json" {
				return writeJSON(cmd.OutOrStdout(), sum)
			}
			return summary.Markdown(cmd.OutOrStdout(), sum)
		},
	}
	cmd.Flags().StringVarP(&name, "context", "c", "", "context to summarize (default: current context)")
	return cmd
}
