package summary

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kubestrata/kubestrata/internal/snapshot"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// fixture is a synthetic OpenShift snapshot: two application namespaces, a system one, a
// DeploymentConfig and an ingress-nginx Ingress at end of life, an orphan pod and a ReplicaSet
// whose owner was not collected, platform kinds and Argo CD with nothing attributed.
func fixture() *snapshot.Snapshot {
	t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	r := func(group, kind, ns, name, cat, mgr, team string, facts map[string]any) snapshot.Resource {
		return snapshot.Resource{ID: snapshot.ID(group, kind, ns, name), Group: group, Kind: kind, Cat: cat, NS: ns,
			Name: name, Created: t0.AddDate(-1, 0, 0), Manager: mgr, Team: team, Facts: facts}
	}
	s := &snapshot.Snapshot{SchemaVersion: snapshot.SchemaVersion, CollectedAt: t0,
		Tool: snapshot.Tool{Name: "kubestrata", Version: "test"},
		Cluster: snapshot.Cluster{ID: "0123456789ab", Context: "source", Distro: "openshift", Version: "v1.37.0",
			Nodes: snapshot.NodeSummary{Count: 3, Arch: map[string]int{"amd64": 3}, InstanceTypes: map[string]int{"m6a.large": 3},
				Zones: []string{"zone-a", "zone-b"}, ZoneCounts: map[string]int{"zone-a": 2, "zone-b": 1}},
			ArgoCD: &snapshot.ArgoCD{Applications: 2}},
		Scope: snapshot.Scope{SkippedKinds: []string{"core/Secret"}},
		Namespaces: []snapshot.Namespace{{Name: "ns-a", Team: "team-a"}, {Name: "ns-b"},
			{Name: "openshift-monitoring", System: true}},
		Resources: []snapshot.Resource{
			r("apps", "Deployment", "ns-a", "api", "workload", "helm", "team-a", nil),
			r("apps.openshift.io", "DeploymentConfig", "ns-a", "portal", "workload", "kubectl", "team-a",
				map[string]any{"end_of_life": "DeploymentConfig is deprecated since OpenShift 4.14; target: Deployment"}),
			r("networking.k8s.io", "Ingress", "ns-a", "api", "network", "helm", "team-a",
				map[string]any{"end_of_life": "ingress-nginx was retired in March 2026; target: Gateway API"}),
			r("", "Service", "ns-a", "api", "network", "helm", "team-a", nil),
			r("", "Pod", "ns-b", "debug", "workload", "console", "", map[string]any{"orphan": true}),
			r("apps", "ReplicaSet", "ns-b", "old-7f9c", "workload", "other", "", map[string]any{"owner_missing": true}),
			r("route.openshift.io", "Route", "ns-b", "web", "network", "kubectl", "", nil),
			r("storage.k8s.io", "StorageClass", "", "thin", "storage", "platform", "", nil),
			r("", "ConfigMap", "openshift-monitoring", "x", "config", "platform", "", nil),
		},
		Errors: []snapshot.Error{{Kind: "argoproj.io/Application", Reason: "Forbidden"}},
	}
	mc := r("machineconfiguration.openshift.io", "MachineConfig", "", "00-worker", "crd", "platform", "", nil)
	mc.Platform = true
	s.Resources = append(s.Resources, mc)
	s.Normalize()
	return s
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/summary -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file (go test ./internal/summary -update rewrites it):\n%s", name, got)
	}
}

func TestGoldenMarkdown(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, Of(fixture())); err != nil {
		t.Fatal(err)
	}
	golden(t, "summary.md", b.Bytes())
}

func TestGoldenJSON(t *testing.T) {
	b, err := json.MarshalIndent(Of(fixture()), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "summary.json", append(b, '\n'))
}

func TestCounts(t *testing.T) {
	s := Of(fixture())
	if s.Resources != 9 || s.SystemResources != 1 {
		t.Errorf("resources %d, system %d", s.Resources, s.SystemResources)
	}
	if len(s.Unowned) != 2 || s.Unowned[0].Reason != "owner_missing" || s.Unowned[1].Reason != "orphan" {
		t.Errorf("unowned: %+v", s.Unowned)
	}
	if len(s.EndOfLife) != 2 || len(s.TopNamespaces) != 2 || s.TopNamespaces[0].NS != "ns-a" || s.TopNamespaces[0].Team != "team-a" {
		t.Errorf("eol %+v, top %+v", s.EndOfLife, s.TopNamespaces)
	}
	if s.Categories[0] != (Count{Name: "workload", Count: 4}) {
		t.Errorf("categories: %+v", s.Categories)
	}
	found := false
	for _, c := range s.Categories {
		found = found || c == Count{Name: "platform", Count: 1}
	}
	if !found {
		t.Errorf("platform kinds not counted apart: %+v", s.Categories)
	}
	// deterministic
	a, _ := json.Marshal(Of(fixture()))
	b, _ := json.Marshal(Of(fixture()))
	if !bytes.Equal(a, b) {
		t.Error("two summaries of the same snapshot differ")
	}
}

// Cluster strings cannot break a table row or add markup.
func TestMarkdownEscapes(t *testing.T) {
	if got := md("a|b`c\nd<e>"); got != `a\|b'c d&lt;e&gt;` {
		t.Errorf("md = %q", got)
	}
}
