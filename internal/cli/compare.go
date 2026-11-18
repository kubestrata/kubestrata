package cli

import (
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/leosavio/kubestrata/internal/compare"
	"github.com/leosavio/kubestrata/internal/render"
	"github.com/leosavio/kubestrata/internal/snapshot"
)

type compareDoc struct {
	*compare.Result
	Index string `json:"index"`
	URL   string `json:"url"`
}

func newCompareCmd(g *globals) *cobra.Command {
	var contexts []string
	var out string
	var open bool
	o := render.Options{Mode: "compare"}
	cmd := &cobra.Command{
		Use:   "compare -c <source> -c <target>",
		Short: "Compare two cached snapshots side by side (no cluster access)",
		Long: "compare reads the newest cached snapshot of a source and a target context and pairs their resources " +
			"by group/Kind/namespace/name: equal (same fingerprint), drift (different fingerprint), pending " +
			"(only in the source) or new (only in the target). It prints the coverage per namespace, " +
			"(equal + drift) / source, and writes a side-by-side site. Run `collect` first.",
		Example: "  kubestrata compare -c source -c target --open\n" +
			"  kubestrata compare -c source -c target --format json > compare.json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := g.checkFormat(); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if len(contexts) != 2 || contexts[0] == contexts[1] {
				return fmt.Errorf("compare needs two different contexts: -c <source> -c <target>")
			}
			c, err := g.openCache()
			if err != nil {
				return err
			}
			var snaps []*snapshot.Snapshot
			for _, name := range contexts {
				s, err := loadLatest(c, name)
				if err != nil {
					return err
				}
				snaps = append(snaps, s)
			}
			if snaps[0].Cluster.ID == snaps[1].Cluster.ID {
				return fmt.Errorf("%s and %s point to the same cluster (%s)", contexts[0], contexts[1], snaps[0].Cluster.ID)
			}
			res := compare.Compare(snaps[0], snaps[1])
			if out == "" {
				out = filepath.Join(c.Dir, "compare")
			}
			o.Version, o.Compare = g.version, res
			index, err := render.Write(out, snaps, o)
			if err != nil {
				return err
			}
			abs, _ := filepath.Abs(index)
			url := "file://" + abs
			w := cmd.OutOrStdout()
			if g.format == "json" {
				if err := writeJSON(w, compareDoc{Result: res, Index: abs, URL: url}); err != nil {
					return err
				}
			} else if err := printCompare(w, res, url); err != nil {
				return err
			}
			if open {
				return openBrowser(abs)
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&contexts, "context", "c", nil, "source context, then target context")
	cmd.Flags().StringVarP(&out, "output", "o", "", "output directory (default: <cache dir>/compare)")
	cmd.Flags().BoolVar(&open, "open", false, "open the page in the default browser")
	cmd.Flags().StringVar(&o.Scale, "scale", "30d", "initial ring scale: 5d, 10d, 30d, log or custom")
	cmd.Flags().StringVar(&o.Axis, "axis", "created", "initial time axis: created or changed")
	cmd.Flags().IntSliceVar(&o.Rings, "rings", nil, "custom rings in days, ascending (e.g. 5,15,30,90,180,365)")
	return cmd
}

func printCompare(w io.Writer, r *compare.Result, url string) error {
	side := func(label string, s compare.Side) string {
		return fmt.Sprintf("%s  %s (%s, %s %s), collected %s, %d resources", label, s.Context, s.ClusterID,
			s.Distro, s.Version, s.CollectedAt.Format(time.RFC3339), s.Resources)
	}
	t := newTable("NAMESPACE", "TEAM", "SOURCE", "TARGET", "EQUAL", "DRIFT", "PENDING", "NEW", "COVERAGE")
	row := func(s compare.Summary, name string) {
		t.Row(name, s.Team, fmt.Sprint(s.Source), fmt.Sprint(s.Target), fmt.Sprint(s.Equal), fmt.Sprint(s.Drift),
			fmt.Sprint(s.Pending), fmt.Sprint(s.New), pct(s.Coverage))
	}
	var system []string
	for _, s := range r.Namespaces {
		switch {
		case s.System:
			system = append(system, s.NS)
		case s.NS == snapshot.ClusterScoped:
			row(s, "(cluster-scoped)")
		default:
			row(s, s.NS)
		}
	}
	row(r.Totals, "total")
	lines := []string{side("source", r.Source), side("target", r.Target), t.String()}
	if len(system) > 0 {
		lines = append(lines, "system namespaces, not in the total: "+strings.Join(system, ", "))
	}
	lines = append(lines, url)
	_, err := lipgloss.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

// pct rounds half up like Math.round in the page (coverage is never negative), so the CLI and
// the page print the same percentage; %.0f alone would round 12.5 to the even 12.
func pct(c *float64) string {
	if c == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", math.Round(*c*100))
}
