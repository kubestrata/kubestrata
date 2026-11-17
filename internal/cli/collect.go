package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/spf13/cobra"

	"github.com/leosavio/kubestrata/internal/cache"
	"github.com/leosavio/kubestrata/internal/collect"
	"github.com/leosavio/kubestrata/internal/kube"
)

// maxParallelContexts caps how many clusters are listed at the same time.
const maxParallelContexts = 4

type collectResult struct {
	Context   string    `json:"context"`
	Status    string    `json:"status"` // collected | fresh | failed
	ClusterID string    `json:"cluster_id,omitempty"`
	Distro    string    `json:"distro,omitempty"`
	Version   string    `json:"version,omitempty"`
	Resources int       `json:"resources"`
	Errors    int       `json:"errors"`
	Collected time.Time `json:"collected_at,omitzero"`
	Seconds   float64   `json:"seconds"`
	Path      string    `json:"path,omitempty"`
	Message   string    `json:"message,omitempty"`
}

type collectDoc struct {
	SchemaVersion int             `json:"schema_version"`
	Results       []collectResult `json:"results"`
}

type collectFlags struct {
	contexts []string
	all      bool
	maxAge   time.Duration
	force    bool
	crds     bool
	systemNS string
	teamKeys []string
}

func newCollectCmd(g *globals) *cobra.Command {
	f := &collectFlags{}
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect a read-only inventory of clusters into the local cache",
		Long: "collect lists the selected contexts with list/get calls only and writes a sanitized snapshot " +
			"per cluster to the local cache. A snapshot newer than --max-age is reused unless --force is set.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := g.checkFormat(); err != nil {
				return err
			}
			targets, err := g.resolveContexts(f.contexts, f.all)
			if err != nil {
				return err
			}
			c, err := g.openCache()
			if err != nil {
				return err
			}
			results := runCollect(cmd.Context(), g, c, f, targets)
			if err := printCollect(cmd, g, results); err != nil {
				return err
			}
			var failed []string
			for _, r := range results {
				if r.Status == "failed" {
					failed = append(failed, r.Context)
				}
			}
			if len(failed) > 0 {
				return fmt.Errorf("collection failed for %s", strings.Join(failed, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&f.contexts, "context", "c", nil, "context to collect (repeatable; default: current context)")
	cmd.Flags().BoolVar(&f.all, "all", false, "collect every context in the kubeconfig")
	cmd.Flags().DurationVar(&f.maxAge, "max-age", 6*time.Hour, "reuse a cached snapshot younger than this")
	cmd.Flags().BoolVar(&f.force, "force", false, "ignore --max-age and collect again (refresh)")
	cmd.Flags().BoolVar(&f.crds, "crds", true, "include custom resources")
	cmd.Flags().StringVar(&f.systemNS, "system-ns", collect.DefaultSystemNS, "regexp of system namespaces (hidden by default in the view)")
	cmd.Flags().StringSliceVar(&f.teamKeys, "team-label", collect.DefaultTeamKeys, "label/annotation keys that name the owning team, in order")
	return cmd
}

func runCollect(ctx context.Context, g *globals, c *cache.Cache, f *collectFlags, targets []string) []collectResult {
	results := make([]collectResult, len(targets))
	sem := make(chan struct{}, maxParallelContexts)
	var wg sync.WaitGroup
	for i, name := range targets {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = collectOne(ctx, g, c, f, name)
		}(i, name)
	}
	wg.Wait()
	return results
}

func collectOne(ctx context.Context, g *globals, c *cache.Cache, f *collectFlags, name string) collectResult {
	start := time.Now()
	r := collectResult{Context: name}
	if !f.force {
		if e, ok, err := c.Latest(name); err == nil && ok && time.Since(e.CollectedAt) < f.maxAge {
			r.Status, r.ClusterID, r.Distro, r.Version = "fresh", e.ClusterID, e.Distro, e.Version
			r.Resources, r.Errors, r.Collected, r.Path = e.Resources, e.Errors, e.CollectedAt, e.Path
			return r
		}
	}
	fail := func(err error) collectResult {
		r.Status, r.Message, r.Seconds = "failed", err.Error(), time.Since(start).Seconds()
		return r
	}
	src, err := kube.Open(g.kubeconfig, name, "kubestrata/"+g.version)
	if err != nil {
		return fail(err)
	}
	snap, err := collect.Collect(ctx, src, collect.Options{Context: name, ToolVersion: g.version,
		SystemNS: f.systemNS, TeamKeys: f.teamKeys, CRDs: f.crds})
	if err != nil {
		return fail(err)
	}
	e, err := c.Put(snap)
	if err != nil {
		return fail(err)
	}
	r.Status, r.ClusterID, r.Distro, r.Version = "collected", snap.Cluster.ID, snap.Cluster.Distro, snap.Cluster.Version
	r.Resources, r.Errors, r.Collected, r.Path = len(snap.Resources), len(snap.Errors), snap.CollectedAt, e.Path
	r.Seconds = time.Since(start).Seconds()
	return r
}

func printCollect(cmd *cobra.Command, g *globals, results []collectResult) error {
	out := cmd.OutOrStdout()
	if g.format == "json" {
		return writeJSON(out, collectDoc{SchemaVersion: SchemaVersion, Results: results})
	}
	t := newTable("CONTEXT", "STATUS", "CLUSTER", "DISTRO", "VERSION", "RESOURCES", "ERRORS", "TIME")
	for _, r := range results {
		status := r.Status
		if r.Message != "" {
			status += ": " + truncate(r.Message, 60)
		}
		t.Row(r.Context, status, r.ClusterID, r.Distro, r.Version, fmt.Sprint(r.Resources), fmt.Sprint(r.Errors),
			fmt.Sprintf("%.1fs", r.Seconds))
	}
	_, err := lipgloss.Fprintln(out, t.String())
	return err
}

func newTable(headers ...string) *table.Table {
	head := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	cell := lipgloss.NewStyle().Padding(0, 1)
	return table.New().Border(lipgloss.NormalBorder()).Headers(headers...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return head
			}
			return cell
		})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// resolveContexts validates explicit names against the kubeconfig; default is the current one.
func (g *globals) resolveContexts(names []string, all bool) ([]string, error) {
	ctxs, current, err := kube.LoadContexts(g.kubeconfig)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	var every []string
	for _, c := range ctxs {
		known[c.Name] = true
		every = append(every, c.Name)
	}
	switch {
	case all:
		return every, nil
	case len(names) == 0:
		if current == "" {
			return nil, fmt.Errorf("no current context: pass -c <context> or --all")
		}
		return []string{current}, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if !known[n] {
			return nil, fmt.Errorf("context %q not found in kubeconfig", n)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (g *globals) openCache() (*cache.Cache, error) {
	dir := g.cacheDir
	if dir == "" {
		d, err := cache.DefaultDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	return cache.Open(dir)
}
