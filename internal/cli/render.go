package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/kubestrata/kubestrata/internal/cache"
	"github.com/kubestrata/kubestrata/internal/render"
	"github.com/kubestrata/kubestrata/internal/snapshot"
)

type renderDoc struct {
	SchemaVersion int      `json:"schema_version"`
	Index         string   `json:"index"`
	URL           string   `json:"url"`
	Clusters      []string `json:"clusters"`
}

func newRenderCmd(g *globals) *cobra.Command {
	var contexts, rings []string
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
			if err := scaleOptions(&o, rings); err != nil {
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
				s, err := loadLatest(c, name)
				if err != nil {
					return err
				}
				snaps = append(snaps, s)
				names = append(names, name)
			}
			if out == "" {
				out = filepath.Join(c.Dir, "site")
			}
			if snaps, err = g.snapshots(snaps); err != nil {
				return err
			}
			for i := range names {
				names[i] = g.mask(names[i])
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
	addScaleFlags(cmd, &o, &rings)
	return cmd
}

// addScaleFlags registers --scale, --axis and --rings, shared by render and compare.
func addScaleFlags(cmd *cobra.Command, o *render.Options, rings *[]string) {
	cmd.Flags().StringVar(&o.Scale, "scale", "30d",
		"initial ring scale: 30m, 1h, 5d, 10d, 30d, years (30d to 10y), log, auto (rings at quantiles of the resource ages) or custom")
	cmd.Flags().StringVar(&o.Axis, "axis", "created", "initial time axis: created or changed")
	cmd.Flags().StringSliceVar(rings, "rings", nil,
		"custom rings, ascending: m, h, d or y suffix, a bare number is days (e.g. 30m,2h,1d,7d,30d or 30d,90d,180d,1y,2y,5y)")
}

// scaleOptions reads --rings into the options and validates them.
func scaleOptions(o *render.Options, rings []string) error {
	minutes, err := render.ParseRings(rings)
	if err != nil {
		return err
	}
	o.RingMinutes = minutes
	return o.Validate()
}

// loadLatest reads the newest cached snapshot of a context.
func loadLatest(c *cache.Cache, name string) (*snapshot.Snapshot, error) {
	e, ok, err := c.Latest(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no cached snapshot for %q: run `kubestrata collect -c %s`", name, name)
	}
	return c.Load(e)
}

func openBrowser(path string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, path).Run()
}
