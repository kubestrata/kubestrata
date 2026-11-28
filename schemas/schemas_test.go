package schemas

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kubestrata/kubestrata/internal/cache"
	"github.com/kubestrata/kubestrata/internal/cli"
	"github.com/kubestrata/kubestrata/internal/snapshot"
)

// rich builds snapshots that fill every optional field the schemas describe: an OpenShift source
// (Route, DeploymentConfig, a platform kind, Argo CD, errors, skipped kinds) and an EKS target
// collected by another kubestrata version (HTTPRoute, Deployment, Karpenter-style node summary).
func rich(id, ctx, tool string, at time.Time, target bool) *snapshot.Snapshot {
	r := func(group, kind, ns, name, cat, mgr string, facts map[string]any) snapshot.Resource {
		return snapshot.Resource{ID: snapshot.ID(group, kind, ns, name), Group: group, Version: "v1", Kind: kind, Cat: cat,
			NS: ns, Name: name, Created: at.AddDate(-1, 0, 0), Changed: at.AddDate(0, -1, 0), Manager: mgr, App: name,
			Team: "team-a", Labels: map[string]string{"app": name}, Facts: facts, FP: "sha256:" + kind + name}
	}
	s := &snapshot.Snapshot{SchemaVersion: snapshot.SchemaVersion, CollectedAt: at,
		Tool: snapshot.Tool{Name: "kubestrata", Version: tool},
		Cluster: snapshot.Cluster{ID: id, Context: ctx, ServerHost: "api.lab.example.com:6443", Distro: "openshift",
			DistroSignals: []string{"group route.openshift.io"}, Version: "v1.37.0",
			Nodes: snapshot.NodeSummary{Count: 2, Arch: map[string]int{"amd64": 2}, InstanceTypes: map[string]int{"m6a.large": 2},
				CapacityTypes: map[string]int{"on-demand": 2}, Zones: []string{"zone-a"}, ZoneCounts: map[string]int{"zone-a": 2},
				KubeletVersion: []string{"v1.37.0"}},
			ArgoCD: &snapshot.ArgoCD{Applications: 2, Remote: 1, Managed: 1, FromStatus: 1}},
		Scope: snapshot.Scope{Kinds: []string{"apps/Deployment"}, SkippedKinds: []string{"core/Secret"}, SystemNS: "^kube-system$",
			TeamKeys: []string{"team"}, TeamFromNS: "^team-([a-z]+)-", RequesterIgnore: []string{"kube:admin"}},
		Namespaces: []snapshot.Namespace{{Name: "ns-a", Team: "team-a", TeamFrom: "team", Created: at.AddDate(-2, 0, 0)},
			{Name: "kube-system", System: true, Created: at.AddDate(-2, 0, 0)}},
		Errors: []snapshot.Error{{Kind: "core/Secret", NS: "ns-a", Reason: "Forbidden", Message: "secrets is forbidden"}},
	}
	svc := r("", "Service", "ns-a", "web", "network", "helm", map[string]any{"type": "ClusterIP", "ports": []string{"80/TCP"}})
	if target {
		s.Cluster.Distro = "eks"
		s.Resources = []snapshot.Resource{svc,
			r("gateway.networking.k8s.io", "HTTPRoute", "ns-a", "web", "network", "argocd",
				map[string]any{"hosts": []string{"web.ns-a.example.com"}, "paths": []string{"/"}, "backends": []string{"web:80"}}),
			r("apps", "Deployment", "ns-a", "portal", "workload", "argocd", map[string]any{"images": []string{"reg/portal:2"}, "replicas": 1}),
			r("apps", "Deployment", "ns-a", "fraud", "workload", "argocd", nil)}
	} else {
		mc := r("machineconfiguration.openshift.io", "MachineConfig", "", "00-worker", "crd", "platform", nil)
		mc.Platform = true
		pod := r("", "Pod", "ns-a", "debug", "workload", "console", map[string]any{"orphan": true})
		s.Resources = []snapshot.Resource{svc, mc, pod,
			r("route.openshift.io", "Route", "ns-a", "web", "network", "kubectl",
				map[string]any{"host": "web-ns-a.apps.lab.example.com", "tls_termination": "edge", "backends": []string{"web:80"}}),
			r("apps.openshift.io", "DeploymentConfig", "ns-a", "portal", "workload", "kubectl",
				map[string]any{"images": []string{"reg/portal:1"}, "replicas": 1, "end_of_life": "deprecated"})}
		s.Resources[3].Owner = "core/Service/ns-a/web"
	}
	s.Edges = []snapshot.Edge{{From: s.Resources[1].ID, To: svc.ID, Rel: "routes"}}
	s.Normalize()
	return s
}

func run(t *testing.T, args ...string) []byte {
	t.Helper()
	root := cli.NewRoot("test")
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, errb.String())
	}
	return out.Bytes()
}

// Every JSON output of the CLI, and the files it writes, validates against its schema.
func TestOutputsMatchSchemas(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, s := range []*snapshot.Snapshot{
		rich("aaaaaaaaaaaa", "source", "0.1.4", now.Add(-time.Hour), false),
		rich("aaaaaaaaaaaa", "source", "test", now, false),
		rich("bbbbbbbbbbbb", "target", "0.1.4", now, true),
	} {
		if _, err := c.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	// the diff needs a change between the two source snapshots
	e, _, _ := c.Latest("source")
	latest, _ := c.Load(e)
	latest.Resources[0].Facts["replicas"] = 3
	latest.Resources[0].FP = "sha256:changed"
	latest.CollectedAt = latest.CollectedAt.Add(time.Second)
	if _, err := c.Put(latest); err != nil {
		t.Fatal(err)
	}

	kc := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(kc, []byte(`apiVersion: v1
kind: Config
current-context: source
clusters:
- name: c
  cluster: {server: "https://127.0.0.1:1", insecure-skip-tls-verify: true}
users:
- name: u
  user: {token: not-a-real-token}
contexts:
- name: source
  context: {cluster: c, user: u, namespace: ns-a}
- name: target
  context: {cluster: c, user: u}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	site, snapOut := t.TempDir(), filepath.Join(t.TempDir(), "s.json")
	g := []string{"--cache-dir", dir, "--kubeconfig", kc, "--format", "json"}
	docs := map[string][]byte{
		"contexts": run(t, append([]string{"contexts"}, g...)...),
		"collect":  run(t, append([]string{"collect", "-c", "source", "-c", "target", "--snapshot-out", t.TempDir()}, g...)...),
		"cache-ls": run(t, append([]string{"cache", "ls"}, g...)...),
		"render":   run(t, append([]string{"render", "-c", "source", "-o", filepath.Join(site, "r")}, g...)...),
		"compare":  run(t, append([]string{"compare", "-c", "source", "-c", "target", "-o", filepath.Join(site, "c")}, g...)...),
		"diff":     run(t, append([]string{"diff", "-c", "source"}, g...)...),
		"summary":  run(t, append([]string{"summary", "-c", "source"}, g...)...),
	}
	run(t, append([]string{"collect", "-c", "source", "--snapshot-out", snapOut}, g...)...)
	for name, doc := range docs {
		if err := Check(name, doc); err != nil {
			t.Errorf("%v\n%s", err, doc)
		}
	}
	files := map[string]string{
		snapOut: "snapshot",
		filepath.Join(site, "c", "json", "compare.json"):      "compare",
		filepath.Join(site, "c", "json", "aaaaaaaaaaaa.json"): "snapshot",
		filepath.Join(site, "r", "json", "aaaaaaaaaaaa.json"): "snapshot",
	}
	for p, name := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := Check(name, b); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	// the fixtures reach the optional parts of the schemas
	for _, want := range []string{`"translation": "Route→HTTPRoute"`, `"collector_change"`, `"diff": [`} {
		if !bytes.Contains(docs["compare"], []byte(want)) {
			t.Errorf("compare output misses %s", want)
		}
	}
	if !bytes.Contains(docs["diff"], []byte(`"state": "drift"`)) {
		t.Errorf("diff output without a drift:\n%s", docs["diff"])
	}
}

// The checker catches what the schemas forbid: a field missing from the schema, a missing
// required field, a wrong type and a value outside an enum.
func TestCheckRejects(t *testing.T) {
	for doc, want := range map[string]string{
		`{"schema_version": 1, "index": "i", "url": "u", "clusters": [], "extra": 1}`: `"extra" is not in the schema`,
		`{"schema_version": 1, "index": "i", "clusters": []}`:                         `missing required property "url"`,
		`{"schema_version": 2, "index": "i", "url": "u", "clusters": []}`:             `want 1`,
		`{"schema_version": 1, "index": 3, "url": "u", "clusters": []}`:               `type integer, want string`,
	} {
		if err := Check("render", []byte(doc)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", doc, err, want)
		}
	}
	if err := Check("collect", []byte(`{"schema_version": 1, "results": [{"context": "c", "status": "done", "resources": 0, "errors": 0, "seconds": 0}]}`)); err == nil ||
		!strings.Contains(err.Error(), "is not one of") {
		t.Errorf("enum: %v", err)
	}
	if got := Names(); len(got) != 8 {
		t.Errorf("schemas: %v", got)
	}
}
