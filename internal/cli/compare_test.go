package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kubestrata/kubestrata/internal/cache"
	"github.com/kubestrata/kubestrata/internal/snapshot"
)

func seedCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	c, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	mk := func(id, ctx, image string, extra ...snapshot.Resource) {
		s := &snapshot.Snapshot{SchemaVersion: snapshot.SchemaVersion, CollectedAt: at,
			Cluster:    snapshot.Cluster{ID: id, Context: ctx, Distro: "kind", Version: "v1.37.0"},
			Namespaces: []snapshot.Namespace{{Name: "payments", Team: "team-pay", Created: at}},
			Resources: append([]snapshot.Resource{{ID: "apps/Deployment/payments/api", Group: "apps", Version: "v1",
				Kind: "Deployment", Cat: "workload", NS: "payments", Name: "api", Created: at, Manager: "helm",
				Facts: map[string]any{"images": []string{image}}, FP: "sha256:" + image}}, extra...)}
		s.Normalize()
		if _, err := c.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	mk("aaaaaaaaaaaa", "source", "api:2.56", snapshot.Resource{ID: "core/Service/payments/api", Version: "v1",
		Kind: "Service", Cat: "network", NS: "payments", Name: "api", Created: at, Manager: "helm", FP: "sha256:svc"})
	mk("bbbbbbbbbbbb", "target", "api:2.57")
	return dir
}

func TestCompareJSON(t *testing.T) {
	dir := seedCache(t)
	site := filepath.Join(t.TempDir(), "site")
	out, err := run(t, "compare", "--cache-dir", dir, "-c", "source", "-c", "target", "-o", site, "--format", "json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var doc struct {
		SchemaVersion int    `json:"schema_version"`
		URL           string `json:"url"`
		Source        struct {
			Context string `json:"context"`
		} `json:"source"`
		Totals struct {
			Drift, Pending int
			Coverage       float64
		} `json:"totals"`
		Items []struct {
			ID, State string
			Diff      []struct{ Fact string }
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if doc.SchemaVersion != 1 || doc.Source.Context != "source" || doc.Totals.Drift != 1 || doc.Totals.Pending != 1 ||
		doc.Totals.Coverage != 0.5 || !strings.HasSuffix(doc.URL, "/site/index.html") {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	if len(doc.Items) != 2 || doc.Items[0].State != "drift" || len(doc.Items[0].Diff) != 1 || doc.Items[0].Diff[0].Fact != "images" {
		t.Fatalf("items: %+v", doc.Items)
	}
}

func TestCompareText(t *testing.T) {
	dir := seedCache(t)
	out, err := run(t, "compare", "--cache-dir", dir, "-c", "source", "-c", "target", "-o", t.TempDir())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, s := range []string{"source  source (aaaaaaaaaaaa", "COVERAGE", "payments", "team-pay", "50%", "total", "file://"} {
		if !strings.Contains(out, s) {
			t.Errorf("text output misses %q:\n%s", s, out)
		}
	}
}

// The page uses Math.round(c * 100): halves round up there and must round up here too.
func TestPct(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, tc := range []struct {
		name string
		in   *float64
		want string
	}{
		{"null", nil, "—"},
		{"0", f(0), "0%"},
		{"0.125", f(0.125), "13%"},
		{"0.625", f(0.625), "63%"},
		{"0.4188", f(0.4188), "42%"},
		{"1", f(1), "100%"},
	} {
		if got := pct(tc.in); got != tc.want {
			t.Errorf("pct(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCompareArgs(t *testing.T) {
	dir := seedCache(t)
	for _, args := range [][]string{
		{"-c", "source"},
		{"-c", "source", "-c", "source"},
		{"-c", "source", "-c", "target", "-c", "other"},
		{"-c", "source", "-c", "missing"},
		{"-c", "source", "-c", "target", "--scale", "7d"},
	} {
		a := append([]string{"compare", "--cache-dir", dir, "-o", t.TempDir()}, args...)
		if _, err := run(t, a...); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
