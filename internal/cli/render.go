package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/leosavio/kubestrata/internal/render"
	"github.com/leosavio/kubestrata/internal/snapshot"
)

type renderDoc struct {
	SchemaVersion int      `json:"schema_version"`
	Index         string   `json:"index"`
	URL           string   `json:"url"`
	Clusters      []string `json:"clusters"`
}

func newRenderCmd(g *globals) *cobra.Command {
	var contexts []string
	var out string
	var open bool
	o := render.Options{}
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Write the offline radial view of cached snapshots (no cluster access)",
		Long: "render reads the newest cached snapshot of each context and writes index.html plus data files. " +
			"Newest resources sit in the center, oldest at the edge. Run `collect` first.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := g.checkFormat(); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			c, err := g.openCache()
			if err != nil {
				return err
			}
			targets := contexts
			if len(targets) == 0 {
				if targets, err = g.resolveContexts(nil, false); err != nil {
					return err
				}
			}
			var snaps []*snapshot.Snapshot
			var names []string
			for _, name := range targets {
				e, ok, err := c.Latest(name)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("no cached snapshot for %q: run `kubestrata collect -c %s`", name, name)
				}
				s, err := c.Load(e)
				if err != nil {
					return err
				}
				snaps = append(snaps, s)
				names = append(names, name)
			}
			if out == "" {
				out = filepath.Join(c.Dir, "site")
			}
			o.Version = g.version
			index, err := render.Write(out, snaps, o)
			if err != nil {
				return err
			}
			abs, _ := filepath.Abs(index)
			url := "file://" + abs
			if g.format == "json" {
				if err := writeJSON(cmd.OutOrStdout(), renderDoc{SchemaVersion: SchemaVersion, Index: abs, URL: url, Clusters: names}); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", url)
			}
			if open {
				return openBrowser(abs)
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&contexts, "context", "c", nil, "context to render (repeatable; default: current context)")
	cmd.Flags().StringVarP(&out, "output", "o", "", "output directory (default: <cache dir>/site)")
	cmd.Flags().BoolVar(&open, "open", false, "open the page in the default browser")
	cmd.Flags().StringVar(&o.Scale, "scale", "30d", "initial ring scale: 5d, 10d, 30d, log or custom")
	cmd.Flags().StringVar(&o.Axis, "axis", "created", "initial time axis: created or changed")
	cmd.Flags().IntSliceVar(&o.Rings, "rings", nil, "custom rings in days, ascending (e.g. 5,15,30,90,180,365)")
	return cmd
}

func openBrowser(path string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, path).Run()
}
