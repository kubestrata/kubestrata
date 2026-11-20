package cli

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/leosavio/kubestrata/internal/cache"
)

func newCacheCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "cache", Short: "Inspect and prune cached snapshots"}
	cmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List cached snapshots, newest first (STAMP feeds diff --from/--to)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := g.checkFormat(); err != nil {
				return err
			}
			c, err := g.openCache()
			if err != nil {
				return err
			}
			all, err := c.List()
			if err != nil {
				return err
			}
			for i := range all {
				all[i].Context = g.mask(all[i].Context)
			}
			if g.format == "json" {
				return writeJSON(cmd.OutOrStdout(), struct {
					SchemaVersion int           `json:"schema_version"`
					Dir           string        `json:"dir"`
					Snapshots     []cache.Entry `json:"snapshots"`
				}{SchemaVersion, c.Dir, all})
			}
			// STAMP is what `kubestrata diff --from/--to` takes
			t := newTable("CLUSTER", "CONTEXT", "STAMP", "COLLECTED", "AGE")
			for _, e := range all {
				t.Row(e.ClusterID, e.Context, e.Stamp, e.CollectedAt.Format(time.RFC3339), time.Since(e.CollectedAt).Round(time.Minute).String())
			}
			_, err = lipgloss.Fprintln(cmd.OutOrStdout(), c.Dir+"\n"+t.String())
			return err
		},
	})
	var keep int
	prune := &cobra.Command{
		Use:   "prune",
		Short: "Keep only the newest snapshots of each cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := g.openCache()
			if err != nil {
				return err
			}
			removed, err := c.Prune(keep)
			for _, p := range removed {
				fmt.Fprintln(cmd.OutOrStdout(), "removed", p)
			}
			return err
		},
	}
	prune.Flags().IntVar(&keep, "keep", 5, "snapshots to keep per cluster")
	cmd.AddCommand(prune)
	return cmd
}
