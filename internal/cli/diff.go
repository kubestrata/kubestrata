package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/leosavio/kubestrata/internal/cache"
	"github.com/leosavio/kubestrata/internal/compare"
	"github.com/leosavio/kubestrata/internal/redact"
	"github.com/leosavio/kubestrata/internal/snapshot"
)

func newDiffCmd(g *globals) *cobra.Command {
	var name, from, to string
	cmd := &cobra.Command{
		Use:   "diff -c <context> [--from <stamp>] [--to <stamp>]",
		Short: "Compare two cached snapshots of the same cluster (no cluster access)",
		Long: "diff reads two cached snapshots of the cluster behind a context, by default the newest and the one " +
			"before it, and pairs their resources by group/Kind/namespace/name: new (only in --to), gone (only in " +
			"--from), drift (different fingerprint, with the facts that differ) or equal. Stamps come from the " +
			"STAMP column of `kubestrata cache ls`; RFC 3339 times work too. Run `collect --force` at the two " +
			"moments to compare.",
		Example: "  kubestrata diff -c prod\n" +
			"  kubestrata diff -c prod --from 20261120T144809Z --to 20261120T160102Z --format json",
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
			older, newer, err := pickPair(c, name, from, to)
			if err != nil {
				return err
			}
			a, err := c.Load(older)
			if err != nil {
				return err
			}
			b, err := c.Load(newer)
			if err != nil {
				return err
			}
			tl := compare.CompareTimeline(a, b)
			if g.redact {
				if tl, err = redact.Copy(tl); err != nil {
					return err
				}
				tl.Source.Context, tl.Target.Context = redact.Name(tl.Source.Context), redact.Name(tl.Target.Context)
			}
			if g.format == "json" {
				return writeJSON(cmd.OutOrStdout(), tl)
			}
			return printDiff(cmd.OutOrStdout(), tl)
		},
	}
	cmd.Flags().StringVarP(&name, "context", "c", "", "context whose cluster to diff (default: current context)")
	cmd.Flags().StringVar(&from, "from", "", "older snapshot: stamp or RFC 3339 time (default: the one before --to)")
	cmd.Flags().StringVar(&to, "to", "", "newer snapshot: stamp or RFC 3339 time (default: the newest)")
	return cmd
}

// pickPair finds the --from and --to snapshots among the cached snapshots of the context's cluster.
func pickPair(c *cache.Cache, name, from, to string) (cache.Entry, cache.Entry, error) {
	var none cache.Entry
	latest, ok, err := c.Latest(name)
	if err != nil {
		return none, none, err
	}
	if !ok {
		return none, none, fmt.Errorf("no cached snapshot for %q: run `kubestrata collect -c %s`", name, name)
	}
	hist, err := c.History(latest.ClusterID)
	if err != nil {
		return none, none, err
	}
	find := func(flag, s string) (int, error) {
		t, err := cache.ParseStamp(s)
		if err != nil {
			return 0, fmt.Errorf("--%s: %w", flag, err)
		}
		for i, e := range hist {
			if e.CollectedAt.Equal(t) {
				return i, nil
			}
		}
		return 0, fmt.Errorf("--%s %s: no snapshot of cluster %s at that time (see `kubestrata cache ls`)", flag, s, latest.ClusterID)
	}
	ti := 0 // newest first
	if to != "" {
		if ti, err = find("to", to); err != nil {
			return none, none, err
		}
	}
	fi := ti + 1
	if from != "" {
		if fi, err = find("from", from); err != nil {
			return none, none, err
		}
	}
	switch {
	case fi == ti:
		return none, none, fmt.Errorf("--from and --to name the same snapshot")
	case fi >= len(hist):
		return none, none, fmt.Errorf("cluster %s (%s) has %d cached snapshot(s) up to --to: run `kubestrata collect -c %s --force` again",
			latest.ClusterID, name, len(hist), name)
	}
	return hist[fi], hist[ti], nil
}

func printDiff(w io.Writer, t *compare.Timeline) error {
	side := func(label string, s compare.Side) string {
		return fmt.Sprintf("%s  %s (%s), collected %s, stamp %s, %d resources", label, s.Context, s.ClusterID,
			s.CollectedAt.Format(time.RFC3339), cache.Stamp(s.CollectedAt), s.Resources)
	}
	lines := []string{side("from", t.Source), side("to  ", t.Target)}
	if note := collectorNote(t.Collector); note != "" {
		lines = append(lines, note)
	}
	if t.Totals.Changes() == 0 {
		lines = append(lines, "no changes outside system namespaces")
	} else {
		sum := newTable("NAMESPACE", "TEAM", "FROM", "TO", "EQUAL", "DRIFT", "GONE", "NEW")
		row := func(s compare.TimelineSummary, name string) {
			sum.Row(name, s.Team, fmt.Sprint(s.Source), fmt.Sprint(s.Target), fmt.Sprint(s.Equal), fmt.Sprint(s.Drift),
				fmt.Sprint(s.Gone), fmt.Sprint(s.New))
		}
		for _, s := range t.Namespaces {
			if s.System || s.Changes() == 0 {
				continue
			}
			label := s.NS
			if s.NS == snapshot.ClusterScoped {
				label = "(cluster-scoped)"
			}
			row(s, label)
		}
		row(t.Totals, "total")
		system := map[string]bool{}
		for _, s := range t.Namespaces {
			system[s.NS] = s.System
		}
		changes := newTable("STATE", "KIND", "NAMESPACE", "NAME", "WHAT CHANGED")
		for _, it := range t.Items {
			ns := it.NS
			if ns == "" {
				ns = snapshot.ClusterScoped
			}
			if it.State == compare.Equal || system[ns] {
				continue
			}
			changes.Row(it.State, it.Kind, ns, nameOf(it.ID), diffText(it.Diff))
		}
		lines = append(lines, sum.String(), changes.String())
	}
	var sys []string
	for _, s := range t.Namespaces {
		if s.System && s.Changes() > 0 {
			sys = append(sys, fmt.Sprintf("%s (%d)", s.NS, s.Changes()))
		}
	}
	if len(sys) > 0 {
		lines = append(lines, "changes in system namespaces, not listed (see --format json): "+strings.Join(sys, ", "))
	}
	_, err := lipgloss.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

// collectorNote explains a comparison of snapshots taken by different kubestrata versions.
func collectorNote(c *compare.CollectorChange) string {
	if c == nil {
		return ""
	}
	from, to := c.From, c.To
	if from == "" {
		from = "unknown"
	}
	if to == "" {
		to = "unknown"
	}
	return fmt.Sprintf("note: the snapshots come from different kubestrata versions (%s → %s), so a fact only one of them "+
		"has is not counted as drift; resources that differ only in such facts: %d (collector_facts in --format json)", from, to, c.Resources)
}

// nameOf is the last segment of an id group/Kind/namespace/name.
func nameOf(id string) string {
	return id[strings.LastIndexByte(id, '/')+1:]
}

// diffText writes the facts of a drift as "fact: old → new", values cut at 60 characters.
func diffText(d []compare.Diff) string {
	parts := make([]string, 0, len(d))
	for _, x := range d {
		parts = append(parts, x.Fact+": "+value(x.Source)+" → "+value(x.Target))
	}
	return strings.Join(parts, "; ")
}

func value(v any) string {
	var s string
	switch x := v.(type) {
	case nil:
		return "—"
	case string:
		s = x
	case []any:
		items := make([]string, 0, len(x))
		for _, i := range x {
			items = append(items, value(i))
		}
		s = strings.Join(items, ",")
	case []string:
		s = strings.Join(x, ",")
	default:
		b, _ := json.Marshal(x)
		s = string(b)
	}
	return truncate(s, 60)
}
