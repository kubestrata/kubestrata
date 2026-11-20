package render

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leosavio/kubestrata/internal/compare"
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

func comparePair() (*snapshot.Snapshot, *snapshot.Snapshot) {
	src, tgt := fixture(), fixture()
	tgt.Cluster.ID, tgt.Cluster.Context, tgt.Cluster.Distro = "ba9876543210", "lab-target", "eks"
	tgt.Resources[0].Facts = map[string]any{"images": []string{"reg/api:2.57"}}
	tgt.Resources[0].FP = "sha256:target"
	tgt.Resources = append(tgt.Resources, snapshot.Resource{ID: "apps/Deployment/payments/fraud", Group: "apps",
		Version: "v1", Kind: "Deployment", Cat: "workload", NS: "payments", Name: "fraud", Created: tgt.CollectedAt})
	tgt.Normalize()
	return src, tgt
}

func TestWriteCompareIsDeterministic(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, d := range []string{a, b} {
		src, tgt := comparePair()
		o := Options{Mode: "compare", Version: "test", Compare: compare.Compare(src, tgt)}
		if _, err := Write(d, []*snapshot.Snapshot{src, tgt}, o); err != nil {
			t.Fatal(err)
		}
	}
	fa, fb := readAll(t, a), readAll(t, b)
	// index.html + data and json for both snapshots and for the comparison
	if len(fa) != 7 || fa[filepath.Join("data", "compare.js")] == nil || fa[filepath.Join("json", "compare.json")] == nil {
		t.Fatalf("files: %d", len(fa))
	}
	for name, content := range fa {
		if !bytes.Equal(content, fb[name]) {
			t.Errorf("%s differs between two renders", name)
		}
	}
	page := string(fa["index.html"])
	for _, s := range []string{`"mode":"compare"`, `"clusters":["0123456789ab","ba9876543210"]`,
		`<script src="data/compare.js"></script>`} {
		if !strings.Contains(page, s) {
			t.Errorf("index.html misses %s", s)
		}
	}
	js := fa[filepath.Join("data", "compare.js")]
	if !bytes.HasPrefix(js, []byte("window.KUBESTRATA_COMPARE={")) || bytes.Contains(js, []byte("</script>")) {
		t.Errorf("compare.js: %.80s", js)
	}
}

func TestWriteCompareRefusesBadInput(t *testing.T) {
	src, tgt := comparePair()
	res := compare.Compare(src, tgt)
	cases := map[string]struct {
		snaps []*snapshot.Snapshot
		res   *compare.Result
	}{
		"one snapshot":  {[]*snapshot.Snapshot{src}, res},
		"no comparison": {[]*snapshot.Snapshot{src, tgt}, nil},
		"same cluster":  {[]*snapshot.Snapshot{src, src}, compare.Compare(src, src)},
		"swapped":       {[]*snapshot.Snapshot{tgt, src}, res},
	}
	for name, c := range cases {
		if _, err := Write(t.TempDir(), c.snaps, Options{Mode: "compare", Compare: c.res}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOptions(t *testing.T) {
	bad := []Options{{Scale: "7d"}, {Axis: "deleted"}, {Scale: "custom"}, {RingMinutes: []int{30, 10}},
		{RingMinutes: []int{60, 60}}, {Mode: "diff"}}
	for _, o := range bad {
		if err := o.Validate(); err == nil {
			t.Errorf("accepted %+v", o)
		}
	}
	o := Options{RingMinutes: []int{5, 15, 30}}
	if err := o.Validate(); err != nil || o.Scale != "custom" {
		t.Errorf("rings: scale=%s err=%v", o.Scale, err)
	}
	for _, s := range []string{"30m", "1h", "5d", "10d", "30d", "log"} {
		o := Options{Scale: s}
		if err := o.Validate(); err != nil {
			t.Errorf("scale %s: %v", s, err)
		}
	}
}

func TestParseRings(t *testing.T) {
	got, err := ParseRings([]string{"30m", "2h", "1d", "7", " 30d "})
	want := []int{30, 120, 1440, 7 * 1440, 30 * 1440}
	if err != nil || len(got) != len(want) {
		t.Fatalf("ParseRings = %v, %v", got, err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseRings = %v, want %v", got, want)
		}
	}
	// a bare number keeps meaning days
	if got, _ := ParseRings([]string{"5", "15", "30"}); len(got) != 3 || got[2] != 30*1440 {
		t.Errorf("bare days: %v", got)
	}
	for _, bad := range [][]string{{"0"}, {"-5"}, {"1.5h"}, {"2w"}, {"h"}, {""}, {"99999999999999d"}} {
		if _, err := ParseRings(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got, err := ParseRings(nil); got != nil || err != nil {
		t.Errorf("no rings: %v, %v", got, err)
	}
}

// The ring minutes reach the page config under their own key.
func TestRingMinutesInConfig(t *testing.T) {
	d := t.TempDir()
	if _, err := Write(d, []*snapshot.Snapshot{fixture()}, Options{RingMinutes: []int{30, 120, 1440}}); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(filepath.Join(d, "index.html"))
	if !bytes.Contains(page, []byte(`"scale":"custom"`)) || !bytes.Contains(page, []byte(`"ring_minutes":[30,120,1440]`)) {
		t.Error("index.html misses the custom rings in minutes")
	}
}
