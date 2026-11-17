package render

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leosavio/kubestrata/internal/snapshot"
)

func fixture() *snapshot.Snapshot {
	t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	s := &snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion, CollectedAt: t0,
		Tool:       snapshot.Tool{Name: "kubestrata", Version: "test"},
		Cluster:    snapshot.Cluster{ID: "0123456789ab", Context: "lab", Distro: "kind", Version: "v1.37.0"},
		Namespaces: []snapshot.Namespace{{Name: "payments", Team: "team-payments", Created: t0.AddDate(-1, 0, 0)}},
		Resources: []snapshot.Resource{
			{ID: "apps/Deployment/payments/api", Group: "apps", Version: "v1", Kind: "Deployment", Cat: "workload",
				NS: "payments", Name: "api", Created: t0.AddDate(0, -3, 0), Manager: "helm",
				Facts: map[string]any{"images": []string{"</script><img src=x onerror=alert(1)>"}}},
			{ID: "core/Service/payments/api", Version: "v1", Kind: "Service", Cat: "network", NS: "payments",
				Name: "api", Created: t0.AddDate(0, -3, 0), Manager: "helm"},
		},
		Edges: []snapshot.Edge{{From: "core/Service/payments/api", To: "apps/Deployment/payments/api", Rel: "selects"}},
	}
	s.Normalize()
	return s
}

func readAll(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		out[rel] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWriteIsDeterministic(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, d := range []string{a, b} {
		if _, err := Write(d, []*snapshot.Snapshot{fixture()}, Options{Version: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	fa, fb := readAll(t, a), readAll(t, b)
	if len(fa) != 3 {
		t.Fatalf("files: %v", fa)
	}
	for name, content := range fa {
		if !bytes.Equal(content, fb[name]) {
			t.Errorf("%s differs between two renders", name)
		}
	}
}

func TestWriteEscapesData(t *testing.T) {
	d := t.TempDir()
	if _, err := Write(d, []*snapshot.Snapshot{fixture()}, Options{}); err != nil {
		t.Fatal(err)
	}
	js, _ := os.ReadFile(filepath.Join(d, "data", "0123456789ab.js"))
	if bytes.Contains(js, []byte("</script>")) || bytes.Contains(js, []byte("<img")) {
		t.Fatalf("data file carries raw markup: %s", js)
	}
	page, _ := os.ReadFile(filepath.Join(d, "index.html"))
	for _, ph := range []string{"__CSS__", "__JS__", "__CONFIG__", "__DATA_SCRIPTS__", "__VERSION__"} {
		if bytes.Contains(page, []byte(ph)) {
			t.Errorf("placeholder %s left in index.html", ph)
		}
	}
	if !bytes.Contains(page, []byte(`<script src="data/0123456789ab.js"></script>`)) {
		t.Error("index.html does not load the data file")
	}
	if bytes.Contains(page, []byte("http://")) || bytes.Contains(page, []byte("https://")) {
		t.Error("index.html references a network URL")
	}
	fi, _ := os.Stat(filepath.Join(d, "index.html"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("index.html mode %v", fi.Mode().Perm())
	}
}

func TestOptions(t *testing.T) {
	bad := []Options{{Scale: "7d"}, {Axis: "deleted"}, {Scale: "custom"}, {Rings: []int{30, 10}}}
	for _, o := range bad {
		if err := o.Validate(); err == nil {
			t.Errorf("accepted %+v", o)
		}
	}
	o := Options{Rings: []int{5, 15, 30}}
	if err := o.Validate(); err != nil || o.Scale != "custom" {
		t.Errorf("rings: scale=%s err=%v", o.Scale, err)
	}
	if !strings.Contains(strings.Join(Scales, ","), "log") {
		t.Error("log scale missing")
	}
}
