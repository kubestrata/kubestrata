package render

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kubestrata/kubestrata/internal/compare"
	"github.com/kubestrata/kubestrata/internal/snapshot"
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
	for _, s := range []string{"30m", "1h", "5d", "10d", "30d", "years", "log", "auto"} {
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
	// y is 365 days, for long-lived clusters
	year := 365 * 1440
	got, err = ParseRings([]string{"30d", "90d", "180d", "1y", "2y", "3y", "5y"})
	want = []int{30 * 1440, 90 * 1440, 180 * 1440, year, 2 * year, 3 * year, 5 * year}
	if err != nil || len(got) != len(want) || got[3] != want[3] || got[6] != want[6] {
		t.Errorf("years: %v, %v", got, err)
	}
	if o := (Options{RingMinutes: got}); o.Validate() != nil {
		t.Error("ascending day and year rings rejected")
	}
	if got, err := ParseRings([]string{"100y"}); err != nil || got[0] != 100*year {
		t.Errorf("100y: %v, %v", got, err)
	}
	for _, bad := range [][]string{{"0"}, {"-5"}, {"1.5h"}, {"2w"}, {"h"}, {""}, {"99999999999999d"}, {"101y"}, {"0y"},
		{"1.5y"}, {"y"}} {
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

func aged(ns string, ages ...time.Duration) []snapshot.Resource {
	t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	var out []snapshot.Resource
	for i, a := range ages {
		out = append(out, snapshot.Resource{ID: fmt.Sprintf("core/ConfigMap/%s/c%d", ns, i), Kind: "ConfigMap", Cat: "config",
			NS: ns, Name: fmt.Sprintf("c%d", i), Created: t0.Add(-a), Changed: t0.Add(-a / 2)})
	}
	return out
}

func TestAutoRings(t *testing.T) {
	day := 24 * time.Hour
	s := fixture()
	s.Namespaces = append(s.Namespaces, snapshot.Namespace{Name: "kube-system", System: true})
	// a long-lived cluster: most resources older than a year, the oldest 1500 days; the system
	// namespace (40 years old) is left out
	var ages []time.Duration
	for _, d := range []int{2, 9, 40, 100, 200, 380, 400, 420, 500, 600, 700, 800, 900, 1000, 1200, 1500} {
		ages = append(ages, time.Duration(d)*day)
	}
	s.Resources = append(aged("payments", ages...), aged("kube-system", 40*365*day)...)
	got := AutoRings([]*snapshot.Snapshot{s}, "created")
	const year = 365 * 1440
	if len(got) < 5 || got[len(got)-1] != 4*year {
		t.Fatalf("rings %v: want the last one at 4y, the nice value nearest to 1500 days", got)
	}
	for i, r := range got {
		if i > 0 && r <= got[i-1] {
			t.Fatalf("rings not ascending: %v", got)
		}
		nice := false
		for _, n := range niceMinutes {
			nice = nice || n == r
		}
		if !nice {
			t.Errorf("ring %d minutes is not a rounded value", r)
		}
	}
	if got[0] > 30*1440 {
		t.Errorf("first ring %d minutes: the youngest eighth is days old", got[0])
	}
	// the changed axis halves every age here
	changed := AutoRings([]*snapshot.Snapshot{s}, "changed")
	if changed[len(changed)-1] != 2*year {
		t.Errorf("changed rings %v: want the last one at 2y (750 days)", changed)
	}
	// only system resources: they are used rather than nothing
	sys := fixture()
	sys.Namespaces = []snapshot.Namespace{{Name: "kube-system", System: true}}
	sys.Resources = aged("kube-system", 10*time.Minute, 20*time.Minute)
	if got := AutoRings([]*snapshot.Snapshot{sys}, "created"); len(got) == 0 || got[len(got)-1] != 15 {
		t.Errorf("system-only rings: %v", got)
	}
	empty := fixture()
	empty.Resources = nil
	if got := AutoRings([]*snapshot.Snapshot{empty}, "created"); got != nil {
		t.Errorf("no resources: %v", got)
	}
	// both snapshots of a comparison count, each from its own collected_at
	other := fixture()
	other.Cluster.ID = "ba9876543210"
	other.CollectedAt = other.CollectedAt.Add(2000 * day)
	other.Resources = aged("payments", 0)
	for i := range other.Resources {
		other.Resources[i].Created = other.CollectedAt.Add(-2 * day)
	}
	if got := AutoRings([]*snapshot.Snapshot{s, other}, "created"); got[len(got)-1] != 4*year {
		t.Errorf("two snapshots: %v", got)
	}
}

// The auto rings of both axes reach the page config.
func TestAutoRingsInConfig(t *testing.T) {
	d := t.TempDir()
	if _, err := Write(d, []*snapshot.Snapshot{fixture()}, Options{Scale: "auto"}); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(filepath.Join(d, "index.html"))
	if !bytes.Contains(page, []byte(`"scale":"auto"`)) || !bytes.Contains(page, []byte(`"auto_ring_minutes":{"changed":[`)) ||
		!bytes.Contains(page, []byte(`],"created":[`)) {
		t.Errorf("index.html misses the auto rings: %s", page[bytes.Index(page, []byte("KUBESTRATA_VIEW")):][:300])
	}
}

// --show-system reaches the page config.
func TestShowSystemInConfig(t *testing.T) {
	d := t.TempDir()
	if _, err := Write(d, []*snapshot.Snapshot{fixture()}, Options{ShowSystem: true}); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(filepath.Join(d, "index.html"))
	if !bytes.Contains(page, []byte(`"show_system":true`)) {
		t.Error("index.html misses show_system")
	}
	if _, err := Write(d, []*snapshot.Snapshot{fixture()}, Options{}); err != nil {
		t.Fatal(err)
	}
	if page, _ := os.ReadFile(filepath.Join(d, "index.html")); bytes.Contains(page, []byte(`"show_system":true`)) {
		t.Error("show_system set by default")
	}
}
