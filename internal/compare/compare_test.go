package compare

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/leosavio/kubestrata/internal/snapshot"
)

var t0 = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func res(id, ns, kind, fp string, facts map[string]any) snapshot.Resource {
	return snapshot.Resource{ID: id, NS: ns, Kind: kind, Name: id, Created: t0, FP: fp, Facts: facts}
}

func snaps() (*snapshot.Snapshot, *snapshot.Snapshot) {
	src := &snapshot.Snapshot{SchemaVersion: 1, CollectedAt: t0,
		Cluster: snapshot.Cluster{ID: "aaaaaaaaaaaa", Context: "source", Distro: "openshift", Version: "v1.37.0"},
		Namespaces: []snapshot.Namespace{
			{Name: "payments", Team: "team-pay"}, {Name: "data", Team: "team-data"}, {Name: "kube-system", System: true}},
		Resources: []snapshot.Resource{
			res("apps/Deployment/payments/api", "payments", "Deployment", "sha256:1",
				map[string]any{"images": []string{"reg/api:2.56"}, "replicas": int64(3), "pods": int64(3)}),
			res("apps/Deployment/payments/worker", "payments", "Deployment", "sha256:2",
				map[string]any{"images": []string{"reg/worker:1"}, "pods": int64(1)}),
			res("core/Service/payments/api", "payments", "Service", "sha256:3", nil),
			res("core/ConfigMap/data/db", "data", "ConfigMap", "sha256:4", nil),
			res("core/ConfigMap/kube-system/x", "kube-system", "ConfigMap", "sha256:5", nil),
			res("storage.k8s.io/StorageClass/_/thin", "", "StorageClass", "sha256:6", nil),
		},
	}
	tgt := &snapshot.Snapshot{SchemaVersion: 1, CollectedAt: t0.Add(time.Minute),
		Cluster: snapshot.Cluster{ID: "bbbbbbbbbbbb", Context: "target", Distro: "eks", Version: "v1.37.0"},
		Namespaces: []snapshot.Namespace{
			{Name: "payments"}, {Name: "gateway", Team: "team-edge"}, {Name: "kube-system", System: true}},
		Resources: []snapshot.Resource{
			// drift: the image moved, pods are volatile and must not show in the detail
			res("apps/Deployment/payments/api", "payments", "Deployment", "sha256:9",
				map[string]any{"images": []string{"reg/api:2.57"}, "replicas": int64(3), "pods": int64(1)}),
			// equal: only a volatile fact differs
			res("apps/Deployment/payments/worker", "payments", "Deployment", "sha256:2",
				map[string]any{"images": []string{"reg/worker:1"}, "pods": int64(4)}),
			res("apps/Deployment/payments/fraud", "payments", "Deployment", "sha256:7", nil),
			res("gateway.networking.k8s.io/Gateway/gateway/public", "gateway", "Gateway", "sha256:8", nil),
			res("core/ConfigMap/kube-system/x", "kube-system", "ConfigMap", "sha256:5", nil),
			res("storage.k8s.io/StorageClass/_/gp3", "", "StorageClass", "sha256:6", nil),
		},
	}
	return src, tgt
}

func TestStates(t *testing.T) {
	r := Compare(snaps())
	want := map[string]string{
		"apps/Deployment/payments/api":                     Drift,
		"apps/Deployment/payments/worker":                  Equal,
		"apps/Deployment/payments/fraud":                   New,
		"core/Service/payments/api":                        Pending,
		"core/ConfigMap/data/db":                           Pending,
		"core/ConfigMap/kube-system/x":                     Equal,
		"gateway.networking.k8s.io/Gateway/gateway/public": New,
		"storage.k8s.io/StorageClass/_/thin":               Pending,
		"storage.k8s.io/StorageClass/_/gp3":                New,
	}
	if len(r.Items) != len(want) {
		t.Fatalf("items: %d, want %d", len(r.Items), len(want))
	}
	for id, st := range want {
		it, ok := r.Find(id)
		if !ok || it.State != st {
			t.Errorf("%s: state %q, want %q", id, it.State, st)
		}
	}
	api, _ := r.Find("apps/Deployment/payments/api")
	if len(api.Diff) != 1 || api.Diff[0].Fact != "images" {
		t.Fatalf("drift detail: %+v", api.Diff)
	}
	if _, ok := r.Find("core/Service/payments/none"); ok {
		t.Error("found an id neither side has")
	}
	if r.Source.Context != "source" || r.Target.ClusterID != "bbbbbbbbbbbb" || r.Source.Resources != 6 {
		t.Errorf("sides: %+v %+v", r.Source, r.Target)
	}
}

func TestSummary(t *testing.T) {
	r := Compare(snaps())
	var order []string
	by := map[string]Summary{}
	for _, s := range r.Namespaces {
		order = append(order, s.NS)
		by[s.NS] = s
	}
	if got := len(order); got != 5 || order[0] != "data" || order[len(order)-1] != "_" {
		t.Fatalf("namespace order: %v", order)
	}
	p := by["payments"]
	if p.Source != 3 || p.Target != 3 || p.Equal != 1 || p.Drift != 1 || p.Pending != 1 || p.New != 1 ||
		p.Team != "team-pay" || p.Coverage == nil || *p.Coverage != 0.6667 {
		t.Errorf("payments: %+v cov=%v", p, deref(p.Coverage))
	}
	if d := by["data"]; d.Pending != 1 || d.Coverage == nil || *d.Coverage != 0 {
		t.Errorf("data: %+v", d)
	}
	if g := by["gateway"]; g.Coverage != nil || g.New != 1 || g.Team != "team-edge" {
		t.Errorf("gateway (target only): %+v", g)
	}
	if !by["kube-system"].System {
		t.Error("kube-system not flagged as system")
	}
	// totals leave system namespaces out: payments 3 + data 1 + cluster-scoped 1 in the source
	tot := r.Totals
	if tot.Source != 5 || tot.Target != 5 || tot.Equal != 1 || tot.Drift != 1 || tot.Pending != 3 || tot.New != 3 ||
		tot.Coverage == nil || *tot.Coverage != 0.4 {
		t.Errorf("totals: %+v cov=%v", tot, deref(tot.Coverage))
	}
}

func deref(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

// A snapshot read back from the cache holds float64 and []any; the values are still equal.
func TestDiffFactsAcrossJSON(t *testing.T) {
	fresh := map[string]any{"replicas": int64(2), "images": []string{"a:1"}, "ports": []map[string]any{{"port": 80}}}
	raw, _ := json.Marshal(fresh)
	var read map[string]any
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	if d := diffFacts(fresh, read); len(d) != 0 {
		t.Fatalf("diff after JSON round trip: %+v", d)
	}
	read["replicas"] = float64(3)
	delete(read, "images")
	read["pods"] = float64(9) // volatile
	d := diffFacts(fresh, read)
	if len(d) != 2 || d[0].Fact != "images" || d[0].Target != nil || d[1].Fact != "replicas" {
		t.Fatalf("diff: %+v", d)
	}
}

// Without fingerprints (older snapshots), the relevant facts decide.
func TestNoFingerprint(t *testing.T) {
	src, tgt := snaps()
	src.Resources = []snapshot.Resource{res("core/Service/a/x", "a", "Service", "", map[string]any{"type": "ClusterIP", "pods": 1})}
	tgt.Resources = []snapshot.Resource{res("core/Service/a/x", "a", "Service", "", map[string]any{"type": "ClusterIP", "pods": 2})}
	if it, _ := Compare(src, tgt).Find("core/Service/a/x"); it.State != Equal {
		t.Errorf("state %s, want equal", it.State)
	}
	tgt.Resources[0].Facts["type"] = "LoadBalancer"
	if it, _ := Compare(src, tgt).Find("core/Service/a/x"); it.State != Drift || len(it.Diff) != 1 {
		t.Errorf("state %s diff %+v, want drift on type", it.State, it.Diff)
	}
}

// Snapshots of different kubestrata versions: a fact only one side has is listed in
// collector_facts and does not make the item drift; a fact both sides have still does.
func TestCollectorVersions(t *testing.T) {
	src, tgt := snaps()
	src.Tool.Version, tgt.Tool.Version = "v1", "v2"
	src.Resources = []snapshot.Resource{
		res("karpenter.sh/NodePool/_/a", "", "NodePool", "sha256:1", map[string]any{"limits": []string{"cpu=10"}}),
		res("karpenter.sh/NodePool/_/b", "", "NodePool", "sha256:2", map[string]any{"limits": []string{"cpu=10"}}),
	}
	tgt.Resources = []snapshot.Resource{
		res("karpenter.sh/NodePool/_/a", "", "NodePool", "sha256:3", map[string]any{"limits": []string{"cpu=10"}, "weight": 25}),
		res("karpenter.sh/NodePool/_/b", "", "NodePool", "sha256:4", map[string]any{"limits": []string{"cpu=20"}, "weight": 25}),
	}
	r := Compare(src, tgt)
	a, _ := r.Find("karpenter.sh/NodePool/_/a")
	b, _ := r.Find("karpenter.sh/NodePool/_/b")
	if a.State != Equal || len(a.Collector) != 1 || a.Collector[0] != "weight" || len(a.Diff) != 0 {
		t.Errorf("a: %+v", a)
	}
	if b.State != Drift || len(b.Diff) != 1 || b.Diff[0].Fact != "limits" || len(b.Collector) != 1 {
		t.Errorf("b: %+v", b)
	}
	if r.Collector == nil || r.Collector.From != "v1" || r.Collector.To != "v2" || r.Collector.Resources != 1 ||
		r.Source.Tool != "v1" || r.Target.Tool != "v2" {
		t.Errorf("collector %+v, sides %q %q", r.Collector, r.Source.Tool, r.Target.Tool)
	}
	if tl := CompareTimeline(src, tgt); tl.Collector == nil || tl.Totals.Drift != 1 {
		t.Errorf("timeline: %+v", tl.Totals)
	}
	// Same version: the new fact is a change of the object.
	tgt.Tool.Version = "v1"
	if a, _ := Compare(src, tgt).Find("karpenter.sh/NodePool/_/a"); a.State != Drift || len(a.Collector) != 0 {
		t.Errorf("same version a: %+v", a)
	}
}

func TestDeterministic(t *testing.T) {
	a, _ := json.Marshal(Compare(snaps()))
	src, tgt := snaps()
	// input order must not matter
	for i, j := 0, len(src.Resources)-1; i < j; i, j = i+1, j-1 {
		src.Resources[i], src.Resources[j] = src.Resources[j], src.Resources[i]
	}
	tgt.Namespaces[0], tgt.Namespaces[2] = tgt.Namespaces[2], tgt.Namespaces[0]
	b, _ := json.Marshal(Compare(src, tgt))
	if !bytes.Equal(a, b) {
		t.Fatalf("two comparisons of the same input differ:\n%s\n%s", a, b)
	}
}
