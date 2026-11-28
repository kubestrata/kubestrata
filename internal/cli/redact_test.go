package cli

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kubestrata/kubestrata/internal/cache"
	"github.com/kubestrata/kubestrata/internal/snapshot"
)

const (
	arnSource = "arn:aws:eks:us-west-2:000000000000:cluster/source"
	arnTarget = "arn:aws:eks:us-west-2:000000000000:cluster/target"
	eksHost   = "ABCDEF0123456789ABCDEF0123456789.gr7.us-west-2.eks.amazonaws.com"
)

// leaks names what --redact must hide.
var leaks = []string{"000000000000", "ABCDEF0123456789"}

func assertNoLeak(t *testing.T, where string, b []byte) {
	t.Helper()
	for _, l := range leaks {
		if i := bytes.Index(b, []byte(l)); i >= 0 {
			t.Errorf("%s holds %q near %q", where, l, b[max(0, i-60):min(len(b), i+30)])
		}
	}
}

// seedEKS caches a snapshot per context with the account id in the context name, the endpoint
// host, an ECR image and an error message.
func seedEKS(t *testing.T) (dir, kc string) {
	t.Helper()
	dir = t.TempDir()
	c, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for i, ctx := range []string{arnSource, arnTarget} {
		s := &snapshot.Snapshot{SchemaVersion: snapshot.SchemaVersion, CollectedAt: now,
			Cluster: snapshot.Cluster{ID: []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"}[i], Context: ctx, ServerHost: eksHost,
				Distro: "eks", Version: "v1.36.0"},
			Resources: []snapshot.Resource{deployment("spark", "api",
				"000000000000.dkr.ecr.us-west-2.amazonaws.com/api:"+[]string{"1", "2"}[i], now)},
			Errors: []snapshot.Error{{Kind: "core/Secret", Reason: "Forbidden",
				Message: `secrets is forbidden at https://` + eksHost + `/api/v1/secrets`}}}
		s.Normalize()
		if _, err := c.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	return dir, kubeconfig(t, "https://"+eksHost, arnSource, arnTarget)
}

func TestRedact(t *testing.T) {
	dir, kc := seedEKS(t)
	g := []string{"--cache-dir", dir, "--kubeconfig", kc, "--redact"}
	site := t.TempDir()
	snapOut := filepath.Join(t.TempDir(), "s.json")
	for _, args := range [][]string{
		{"contexts"},
		{"contexts", "--format", "json"},
		{"collect", "-c", arnSource, "--format", "json", "--snapshot-out", snapOut},
		{"collect", "-c", arnSource},
		{"cache", "ls"},
		{"render", "-c", arnSource, "-o", filepath.Join(site, "render"), "--format", "json"},
		{"compare", "-c", arnSource, "-c", arnTarget, "-o", filepath.Join(site, "compare")},
		{"compare", "-c", arnSource, "-c", arnTarget, "-o", filepath.Join(site, "compare-json"), "--format", "json"},
	} {
		out, err := run(t, append(args, g...)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if !strings.Contains(out, "redacted") {
			t.Errorf("%v: no mask in the output:\n%s", args, out)
		}
		assertNoLeak(t, strings.Join(args[:1], " ")+" output", []byte(out))
	}
	b, _ := os.ReadFile(snapOut)
	assertNoLeak(t, "--snapshot-out file", b)
	if !bytes.Contains(b, []byte("redacted.dkr.ecr.us-west-2.amazonaws.com/api:1")) {
		t.Error("--snapshot-out lost the masked image")
	}
	err := filepath.Walk(site, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		assertNoLeak(t, p, b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// the cache keeps the real values
	e, _, _ := (&cache.Cache{Dir: dir}).Latest(arnSource)
	f, _ := os.Open(e.Path)
	defer f.Close()
	zr, _ := gzip.NewReader(f)
	raw, _ := io.ReadAll(zr)
	if !bytes.Contains(raw, []byte(arnSource)) || !bytes.Contains(raw, []byte(eksHost)) {
		t.Error("the cache lost the real values")
	}
	// without --redact nothing is masked
	out, _ := run(t, "contexts", "--kubeconfig", kc)
	if !strings.Contains(out, "000000000000") {
		t.Errorf("contexts masked without --redact:\n%s", out)
	}
}

// Error messages name contexts, and Fang prints them to stderr: --redact masks them too.
func TestRedactErrors(t *testing.T) {
	dir, _ := seedEKS(t)
	uncached := "arn:aws:eks:us-west-2:000000000000:cluster/uncached"
	kc := kubeconfig(t, "https://"+eksHost, arnSource, arnTarget, uncached)
	site := t.TempDir()
	for _, args := range [][]string{
		{"diff", "-c", arnSource}, // one cached snapshot: needs a second collect
		{"diff", "-c", uncached},
		{"render", "-c", uncached, "-o", site},
		{"compare", "-c", arnSource, "-c", uncached, "-o", site},
		{"collect", "-c", "arn:aws:eks:us-west-2:000000000000:cluster/missing"},
		{"diff", arnSource}, // positional argument
	} {
		g := []string{"--cache-dir", dir, "--kubeconfig", kc}
		out, err := run(t, append(args, append(g, "--redact")...)...)
		if err == nil {
			t.Fatalf("%v: no error\n%s", args, out)
		}
		if !strings.Contains(err.Error(), "redacted") {
			t.Errorf("%v: no mask in the error: %v", args, err)
		}
		assertNoLeak(t, fmt.Sprintf("%v error", args), []byte(err.Error()+out))
		// without --redact the same error keeps the real name
		if _, err := run(t, append(args, g...)...); err == nil || !strings.Contains(err.Error(), "000000000000") {
			t.Errorf("%v: error masked without --redact: %v", args, err)
		}
	}
}

func TestRedactDiff(t *testing.T) {
	dir, kc := seedEKS(t)
	c, _ := cache.Open(dir)
	e, _, _ := c.Latest(arnSource)
	s, _ := c.Load(e)
	s.CollectedAt = s.CollectedAt.Add(time.Minute)
	s.Resources[0].Facts["images"] = []string{"000000000000.dkr.ecr.us-west-2.amazonaws.com/api:3"}
	s.Resources[0].FP = "sha256:3"
	if _, err := c.Put(s); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"text", "json"} {
		out, err := run(t, "diff", "-c", arnSource, "--cache-dir", dir, "--kubeconfig", kc, "--redact", "--format", f)
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if !strings.Contains(out, "redacted.dkr.ecr") {
			t.Errorf("diff %s: masked drift missing:\n%s", f, out)
		}
		assertNoLeak(t, "diff "+f, []byte(out))
	}
}

// OpenShift: oc login context names, the API host, Route hosts under apps.<cluster>.<base> and
// teams taken from openshift.io/requester (user names). All values are synthetic.
const (
	ocpSource = "ns-a/api-ocp1-example-com:6443/user-a"
	ocpTarget = "ns-a/api-ocp2-example-com:6443/user-a"
)

var ocpLeaks = []string{"ocp1.example.com", "ocp1-example-com", "ocp2.example.com", "ocp2-example-com", "requester-b"}

func assertNoOCPLeak(t *testing.T, where string, b []byte) {
	t.Helper()
	for _, l := range ocpLeaks {
		if i := bytes.Index(b, []byte(l)); i >= 0 {
			t.Errorf("%s holds %q near %q", where, l, b[max(0, i-60):min(len(b), i+30)])
		}
	}
}

func seedOpenShift(t *testing.T) (dir, kc string) {
	t.Helper()
	dir = t.TempDir()
	c, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for i, ctx := range []string{ocpSource, ocpTarget} {
		domain := []string{"ocp1.example.com", "ocp2.example.com"}[i]
		route := snapshot.Resource{ID: snapshot.ID("route.openshift.io", "Route", "ns-a", "web"), Group: "route.openshift.io",
			Version: "v1", Kind: "Route", Cat: "network", NS: "ns-a", Name: "web", Created: now, Manager: "kubectl",
			Team: "requester-b", Facts: map[string]any{"host": "web-ns-a.apps." + domain}, FP: []string{"sha256:1", "sha256:2"}[i]}
		s := &snapshot.Snapshot{SchemaVersion: snapshot.SchemaVersion, CollectedAt: now,
			Cluster: snapshot.Cluster{ID: []string{"cccccccccccc", "dddddddddddd"}[i], Context: ctx,
				ServerHost: "api." + domain + ":6443", Distro: "openshift", Version: "v1.36.0"},
			Namespaces: []snapshot.Namespace{{Name: "ns-a", Team: "requester-b", TeamFrom: "openshift.io/requester", Created: now},
				{Name: "ns-b", Team: "team-b", TeamFrom: "team", Created: now}},
			Resources: []snapshot.Resource{route},
			Errors: []snapshot.Error{{Kind: "core/Secret", Reason: "Forbidden",
				Message: `Get "https://api.` + domain + `:6443/api/v1/secrets": forbidden`}}}
		s.Normalize()
		if _, err := c.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	return dir, kubeconfig(t, "https://api.ocp1.example.com:6443", ocpSource, ocpTarget)
}

func TestRedactOpenShift(t *testing.T) {
	dir, kc := seedOpenShift(t)
	g := []string{"--cache-dir", dir, "--kubeconfig", kc, "--redact"}
	site := t.TempDir()
	snapOut := filepath.Join(t.TempDir(), "s.json")
	for _, args := range [][]string{
		{"contexts"},
		{"contexts", "--format", "json"},
		{"collect", "-c", ocpSource, "--format", "json", "--snapshot-out", snapOut},
		{"cache", "ls"},
		{"render", "-c", ocpSource, "-o", filepath.Join(site, "render"), "--format", "json"},
		{"compare", "-c", ocpSource, "-c", ocpTarget, "-o", filepath.Join(site, "compare")},
		{"compare", "-c", ocpSource, "-c", ocpTarget, "-o", filepath.Join(site, "compare-json"), "--format", "json"},
	} {
		out, err := run(t, append(args, g...)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if !strings.Contains(out, "redacted") {
			t.Errorf("%v: no mask in the output:\n%s", args, out)
		}
		assertNoOCPLeak(t, strings.Join(args[:1], " ")+" output", []byte(out))
	}
	b, _ := os.ReadFile(snapOut)
	assertNoOCPLeak(t, "--snapshot-out file", b)
	for _, want := range []string{`"web-ns-a.apps.redacted"`, `"api.redacted:6443"`, `"ns-a/api-redacted:6443/user-a"`,
		`"team": "redacted-1"`, `"team": "team-b"`} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("--snapshot-out misses %s", want)
		}
	}
	err := filepath.Walk(site, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		assertNoOCPLeak(t, p, b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// the requester team keeps one mask in both snapshots and in the comparison
	cmp, _ := os.ReadFile(filepath.Join(site, "compare-json", "json", "compare.json"))
	if !bytes.Contains(cmp, []byte(`"team": "redacted-1"`)) {
		t.Errorf("compare.json without the masked team:\n%s", cmp)
	}
	// errors quote the oc login context
	if _, err := run(t, append([]string{"render", "-c", "ns-a/api-ocp1-example-com:6443/user-c", "-o", site}, g...)...); err == nil ||
		!strings.Contains(err.Error(), "api-redacted:6443") || strings.Contains(err.Error(), "ocp1") {
		t.Errorf("render of an unknown context: %v", err)
	}
	// without --redact nothing is masked
	out, _ := run(t, "contexts", "--kubeconfig", kc)
	if !strings.Contains(out, "ocp1-example-com") {
		t.Errorf("contexts masked without --redact:\n%s", out)
	}
}
