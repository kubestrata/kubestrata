package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/spf13/cobra"

	"github.com/kubestrata/kubestrata/internal/cache"
	"github.com/kubestrata/kubestrata/internal/collect"
	"github.com/kubestrata/kubestrata/internal/kube"
	"github.com/kubestrata/kubestrata/internal/snapshot"
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
	// SnapshotOut is the plain JSON copy written by --snapshot-out.
	SnapshotOut string `json:"snapshot_out,omitempty"`
	Message     string `json:"message,omitempty"`
}

type collectDoc struct {
	SchemaVersion int             `json:"schema_version"`
	Results       []collectResult `json:"results"`
}

type collectFlags struct {
	contexts    []string
	all         bool
	maxAge      time.Duration
	force       bool
	crds        bool
	systemNS    string
	teamKeys    []string
	snapshotOut string
	skipKinds   []string
	noSecrets   bool
}

func newCollectCmd(g *globals) *cobra.Command {
	f := &collectFlags{}
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect a read-only inventory of clusters into the local cache",
		Long: "collect lists the selected contexts with list/get calls only and writes a sanitized snapshot " +
			"per cluster to the local cache. A snapshot newer than --max-age is reused unless --force is set. " +
			"--format json prints a summary per context; --snapshot-out also writes the snapshot itself as plain JSON.",
		Example: "  kubestrata collect -c source -c target --force\n" +
			"  kubestrata collect -c source --no-secrets   # role without list on secrets (deploy/rbac)\n" +
			"  kubestrata collect -c source --snapshot-out source.json\n" +
			"  kubestrata collect --all --snapshot-out ./snapshots/   # existing directory: <cluster_id>.json each",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := g.checkFormat(); err != nil {
				return err
			}
			if f.noSecrets {
				f.skipKinds = append(f.skipKinds, "core/Secret")
			}
			if _, err := collect.SkipMatcher(f.skipKinds); err != nil {
				return err
			}
			targets, err := g.resolveContexts(f.contexts, f.all)
			if err != nil {
				return err
			}
			outDir := false
			if f.snapshotOut != "" {
				if fi, err := os.Stat(f.snapshotOut); err == nil && fi.IsDir() {
					outDir = true
				} else if len(targets) > 1 {
					return fmt.Errorf("--snapshot-out %s: %d contexts need an existing directory (one <cluster_id>.json each)",
						f.snapshotOut, len(targets))
				} else if _, err := os.Stat(filepath.Dir(f.snapshotOut)); err != nil {
					return fmt.Errorf("--snapshot-out: %w", err)
				}
			}
			c, err := g.openCache()
			if err != nil {
				return err
			}
			results := runCollect(cmd.Context(), g, c, f, targets)
			var failed, notWritten []string
			for i := range results {
				r := &results[i]
				if r.Status == "failed" {
					failed = append(failed, g.mask(r.Context))
					continue
				}
				if f.snapshotOut == "" {
					continue
				}
				p := f.snapshotOut
				if outDir {
					p = filepath.Join(f.snapshotOut, r.ClusterID+".json")
				}
				if err := g.writeSnapshotOut(c, r.Path, p); err != nil {
					notWritten = append(notWritten, g.mask(r.Context)+": "+err.Error())
					continue
				}
				r.SnapshotOut = p
			}
			if err := printCollect(cmd, g, results); err != nil {
				return err
			}
			if f.snapshotOut != "" && !g.redact && g.format != "json" && len(notWritten) < len(results)-len(failed) {
				// The file leaves the cache and is more likely to be shared than the cache is.
				fmt.Fprintln(cmd.ErrOrStderr(), "note: --snapshot-out keeps the endpoint hosts and account ids of the cluster; "+
					"pass --redact to mask them before sharing the file")
			}
			var errs []string
			if len(failed) > 0 {
				errs = append(errs, "collection failed for "+strings.Join(failed, ", "))
			}
			if len(notWritten) > 0 {
				errs = append(errs, "--snapshot-out not written for "+strings.Join(notWritten, "; "))
			}
			if len(errs) > 0 {
				return errors.New(strings.Join(errs, "; "))
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
	cmd.Flags().StringVar(&f.snapshotOut, "snapshot-out", "",
		"also write the plain JSON snapshot to this file (one context), or into this existing directory as <cluster_id>.json")
	cmd.Flags().StringSliceVar(&f.skipKinds, "skip-kinds", nil,
		"kinds not to list, as Kind or group/Kind with core for the core group (e.g. core/Secret,route.openshift.io/Route); "+
			"recorded in the snapshot scope")
	cmd.Flags().BoolVar(&f.noSecrets, "no-secrets", false,
		"do not list Secrets (same as --skip-kinds core/Secret), for a role without list on secrets: Helm release "+
			"state and links to Secrets are lost")
	return cmd
}

// writeSnapshotOut copies a cached snapshot to p as indented JSON (mode 0600, written
// atomically), redacted under --redact.
func (g *globals) writeSnapshotOut(c *cache.Cache, cached, p string) error {
	s, err := c.Load(cache.Entry{Path: cached})
	if err != nil {
		return err
	}
	if g.redact {
		red, err := g.snapshots([]*snapshot.Snapshot{s})
		if err != nil {
			return err
		}
		s = red[0]
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".kubestrata-snapshot-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
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
		SystemNS: f.systemNS, TeamKeys: f.teamKeys, CRDs: f.crds, SkipKinds: f.skipKinds})
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
	shown := make([]collectResult, len(results))
	for i, r := range results {
		r.Context, r.Message = g.mask(r.Context), g.mask(r.Message)
		shown[i] = r
	}
	if g.format == "json" {
		return writeJSON(out, collectDoc{SchemaVersion: SchemaVersion, Results: shown})
	}
	t := newTable("CONTEXT", "STATUS", "CLUSTER", "DISTRO", "VERSION", "RESOURCES", "ERRORS", "TIME")
	var written []string
	for _, r := range shown {
		status := r.Status
		if r.Message != "" {
			status += ": " + truncate(r.Message, 60)
		}
		t.Row(r.Context, status, r.ClusterID, r.Distro, r.Version, fmt.Sprint(r.Resources), fmt.Sprint(r.Errors),
			fmt.Sprintf("%.1fs", r.Seconds))
		if r.SnapshotOut != "" {
			written = append(written, "snapshot of "+r.Context+": "+r.SnapshotOut)
		}
	}
	_, err := lipgloss.Fprintln(out, strings.Join(append([]string{t.String()}, written...), "\n"))
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
