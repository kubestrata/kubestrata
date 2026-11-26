package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kubestrata/kubestrata/internal/cache"
	"github.com/kubestrata/kubestrata/internal/snapshot"
)

func deployment(ns, name, image string, at time.Time) snapshot.Resource {
	return snapshot.Resource{ID: snapshot.ID("apps", "Deployment", ns, name), Group: "apps", Version: "v1", Kind: "Deployment",
		Cat: "workload", NS: ns, Name: name, Created: at, Manager: "helm",
		Facts: map[string]any{"images": []string{image}}, FP: "sha256:" + image}
}

func configMap(ns, name, fp string, at time.Time) snapshot.Resource {
	return snapshot.Resource{ID: snapshot.ID("", "ConfigMap", ns, name), Version: "v1", Kind: "ConfigMap", Cat: "config",
		NS: ns, Name: name, Created: at, Manager: "kubectl", FP: fp}
}

// putSnap writes one snapshot of cluster id, seen through context ctx, into the cache at dir.
func putSnap(t *testing.T, dir, id, ctx string, at time.Time, res ...snapshot.Resource) {
	t.Helper()
	c, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &snapshot.Snapshot{SchemaVersion: snapshot.SchemaVersion, CollectedAt: at,
		Tool:    snapshot.Tool{Name: "kubestrata", Version: "test"},
		Cluster: snapshot.Cluster{ID: id, Context: ctx, Distro: "eks", Version: "v1.36.0"},
		Namespaces: []snapshot.Namespace{{Name: "spark", Team: "team-data", Created: at},
			{Name: "kube-system", System: true, Created: at}},
		Resources: res}
	s.Normalize()
	if _, err := c.Put(s); err != nil {
		t.Fatal(err)
	}
}

// seedHistory caches two snapshots of one cluster an hour apart: one drift, one gone, one new,
// one equal, and a change in a system namespace.
func seedHistory(t *testing.T) (string, time.Time, time.Time) {
	t.Helper()
	dir := t.TempDir()
	t1 := time.Date(2026, 11, 20, 14, 48, 9, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	putSnap(t, dir, "cccccccccccc", "prod", t1,
		deployment("spark", "api", "api:2.56", t1), deployment("spark", "worker", "worker:1", t1),
		configMap("spark", "old", "sha256:old", t1), configMap("kube-system", "x", "sha256:x1", t1))
	putSnap(t, dir, "cccccccccccc", "prod", t2,
		deployment("spark", "api", "api:2.57", t1), deployment("spark", "worker", "worker:1", t1),
		configMap("spark", "job", "sha256:job", t2), configMap("kube-system", "x", "sha256:x2", t1))
	return dir, t1, t2
}

type diffDoc struct {
	SchemaVersion int    `json:"schema_version"`
	Mode          string `json:"mode"`
	Source        struct {
		CollectedAt time.Time `json:"collected_at"`
	} `json:"source"`
	Target struct {
		CollectedAt time.Time `json:"collected_at"`
	} `json:"target"`
	Totals struct {
		Equal, Drift, Gone, New int
	} `json:"totals"`
	Items []struct {
		ID, State string
		Diff      []struct {
			Fact           string
			Source, Target any
		}
	} `json:"items"`
}

func TestDiffDefaultsToTheTwoNewest(t *testing.T) {
	dir, t1, t2 := seedHistory(t)
	out, err := run(t, "diff", "--cache-dir", dir, "-c", "prod", "--format", "json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var doc diffDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if doc.SchemaVersion != 1 || doc.Mode != "diff" || !doc.Source.CollectedAt.Equal(t1) || !doc.Target.CollectedAt.Equal(t2) {
		t.Fatalf("doc: %+v", doc)
	}
	// totals leave the system namespace out
	if tt := doc.Totals; tt.Equal != 1 || tt.Drift != 1 || tt.Gone != 1 || tt.New != 1 {
		t.Fatalf("totals: %+v", tt)
	}
	states := map[string]string{}
	for _, it := range doc.Items {
		states[it.ID] = it.State
		if it.ID == "apps/Deployment/spark/api" && (len(it.Diff) != 1 || it.Diff[0].Fact != "images") {
			t.Errorf("api drift detail: %+v", it.Diff)
		}
	}
	for id, want := range map[string]string{"apps/Deployment/spark/api": "drift", "apps/Deployment/spark/worker": "equal",
		"core/ConfigMap/spark/old": "gone", "core/ConfigMap/spark/job": "new", "core/ConfigMap/kube-system/x": "drift"} {
		if states[id] != want {
			t.Errorf("%s: %q, want %q", id, states[id], want)
		}
	}

	text, err := run(t, "diff", "--cache-dir", dir, "-c", "prod")
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	for _, s := range []string{"from  prod", "stamp 20261120T144809Z", "stamp 20261120T154809Z", "GONE", "drift",
		"images: api:2.56 → api:2.57", "old", "job", "changes in system namespaces, not listed (see --format json): kube-system (1)"} {
		if !strings.Contains(text, s) {
			t.Errorf("text output misses %q:\n%s", s, text)
		}
	}
	if strings.Contains(text, "worker") {
		t.Errorf("equal resource listed:\n%s", text)
	}
}

func TestDiffStamps(t *testing.T) {
	dir, t1, t2 := seedHistory(t)
	putSnap(t, dir, "cccccccccccc", "prod", t2.Add(time.Hour), deployment("spark", "api", "api:2.58", t1))
	// a stamp and an RFC 3339 time name the same snapshots
	for _, args := range [][]string{
		{"--from", "20261120T144809Z", "--to", "20261120T154809Z"},
		{"--from", t1.Format(time.RFC3339), "--to", t2.Format(time.RFC3339)},
		{"--to", "20261120T154809Z"},
	} {
		out, err := run(t, append([]string{"diff", "--cache-dir", dir, "-c", "prod", "--format", "json"}, args...)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		var doc diffDoc
		if err := json.Unmarshal([]byte(out), &doc); err != nil || !doc.Source.CollectedAt.Equal(t1) || !doc.Target.CollectedAt.Equal(t2) {
			t.Errorf("%v: from %v to %v (err %v)", args, doc.Source.CollectedAt, doc.Target.CollectedAt, err)
		}
	}
	for _, args := range [][]string{
		{"--from", "20261120T144809Z", "--to", "20261120T144809Z"}, // same snapshot
		{"--from", "20250101T000000Z"},                             // not cached
		{"--from", "yesterday"},                                    // not a stamp
		{"--to", "20261120T144809Z"},                               // nothing older
		{"-c", "missing"},
	} {
		a := append([]string{"diff", "--cache-dir", dir, "-c", "prod"}, args...)
		if _, err := run(t, a...); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestDiffNeedsTwoSnapshots(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 20, 14, 0, 0, 0, time.UTC)
	putSnap(t, dir, "dddddddddddd", "one", at)
	out, err := run(t, "diff", "--cache-dir", dir, "-c", "one")
	if err == nil || !strings.Contains(err.Error(), "collect -c one --force") {
		t.Fatalf("want a hint to collect again, got %v\n%s", err, out)
	}
}

func TestCacheLsShowsStamps(t *testing.T) {
	dir, _, _ := seedHistory(t)
	out, err := run(t, "cache", "ls", "--cache-dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"STAMP", "20261120T144809Z", "20261120T154809Z", "prod"} {
		if !strings.Contains(out, s) {
			t.Errorf("cache ls misses %q:\n%s", s, out)
		}
	}
	js, err := run(t, "cache", "ls", "--cache-dir", dir, "--format", "json")
	if err != nil || !strings.Contains(js, `"stamp": "20261120T154809Z"`) {
		t.Errorf("cache ls json: %v\n%s", err, js)
	}
}

// kubeconfig writes a kubeconfig with one context per name, all on server.
func kubeconfig(t *testing.T, server string, names ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Config\ncurrent-context: \"" + names[0] + "\"\nclusters:\n")
	b.WriteString("  - name: c\n    cluster: {server: \"" + server + "\"}\nusers:\n  - name: u\n    user: {token: fake}\ncontexts:\n")
	for _, n := range names {
		b.WriteString("  - name: \"" + n + "\"\n    context: {cluster: c, user: u}\n")
	}
	p := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// collect reuses a fresh cached snapshot, so --snapshot-out runs without a cluster.
func TestSnapshotOut(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	putSnap(t, dir, "aaaaaaaaaaaa", "source", now, deployment("spark", "api", "api:1", now))
	putSnap(t, dir, "bbbbbbbbbbbb", "target", now, deployment("spark", "api", "api:2", now))
	kc := kubeconfig(t, "https://127.0.0.1:6443", "source", "target")
	base := []string{"collect", "--cache-dir", dir, "--kubeconfig", kc}

	file := filepath.Join(t.TempDir(), "source.json")
	out, err := run(t, append(base, "-c", "source", "--snapshot-out", file, "--format", "json")...)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var doc collectDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Results) != 1 || doc.Results[0].Status != "fresh" ||
		doc.Results[0].SnapshotOut != file {
		t.Fatalf("summary: %v %+v", err, doc)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := snapshot.Read(f)
	if err != nil || s.Cluster.ID != "aaaaaaaaaaaa" || len(s.Resources) != 1 {
		t.Fatalf("snapshot file: %v %+v", err, s)
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o600 {
		t.Errorf("snapshot file mode %v", fi.Mode().Perm())
	}

	if _, err := run(t, append(base, "-c", "source", "-c", "target", "--snapshot-out", file)...); err == nil ||
		!strings.Contains(err.Error(), "existing directory") {
		t.Errorf("two contexts into one file: %v", err)
	}
	outDir := t.TempDir()
	text, err := run(t, append(base, "-c", "source", "-c", "target", "--snapshot-out", outDir)...)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	for _, id := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"} {
		p := filepath.Join(outDir, id+".json")
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s not written", p)
		}
		if !strings.Contains(text, p) {
			t.Errorf("text output does not name %s:\n%s", p, text)
		}
	}
	if _, err := run(t, append(base, "-c", "source", "--snapshot-out", filepath.Join(t.TempDir(), "no", "dir.json"))...); err == nil {
		t.Error("accepted a file in a missing directory")
	}
	// Text output warns that the file is not redacted, unless --redact masks it.
	if !strings.Contains(text, "pass --redact to mask them") {
		t.Errorf("no redaction note:\n%s", text)
	}
	red, err := run(t, append(base, "--redact", "-c", "source", "--snapshot-out", file)...)
	if err != nil || strings.Contains(red, "pass --redact") {
		t.Errorf("--redact run: %v\n%s", err, red)
	}
}

// Snapshots of two kubestrata versions: a fact only the newer one collects is not drift.
func TestDiffAcrossCollectorVersions(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 20, 14, 0, 0, 0, time.UTC)
	pool := func(facts map[string]any, fp string) snapshot.Resource {
		return snapshot.Resource{ID: "karpenter.sh/NodePool/_/general", Group: "karpenter.sh", Kind: "NodePool",
			Name: "general", Created: at, Facts: facts, FP: fp}
	}
	for i, s := range []struct {
		version string
		res     snapshot.Resource
	}{
		{"v0.0.0-old", pool(map[string]any{"requirements": []string{"arch In arm64"}}, "sha256:a")},
		{"v0.0.0-new", pool(map[string]any{"requirements": []string{"arch In arm64"}, "weight": 25}, "sha256:b")},
	} {
		snap := &snapshot.Snapshot{SchemaVersion: snapshot.SchemaVersion, CollectedAt: at.Add(time.Duration(i) * time.Hour),
			Tool:      snapshot.Tool{Name: "kubestrata", Version: s.version},
			Cluster:   snapshot.Cluster{ID: "eeeeeeeeeeee", Context: "auto", Distro: "eks", Version: "v1.36.0"},
			Resources: []snapshot.Resource{s.res}}
		snap.Normalize()
		if _, err := c.Put(snap); err != nil {
			t.Fatal(err)
		}
	}
	out, err := run(t, "diff", "--cache-dir", dir, "-c", "auto")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "different kubestrata versions (v0.0.0-old → v0.0.0-new)") ||
		!strings.Contains(out, "resources that differ only in such facts: 1") || !strings.Contains(out, "no changes outside system namespaces") {
		t.Errorf("diff across versions:\n%s", out)
	}
	js, err := run(t, "diff", "--cache-dir", dir, "-c", "auto", "--format", "json")
	if err != nil || !strings.Contains(js, `"collector_facts": [`) || !strings.Contains(js, `"tool": "v0.0.0-new"`) {
		t.Errorf("diff json across versions: %v\n%s", err, js)
	}
}
