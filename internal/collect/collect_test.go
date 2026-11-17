package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/leosavio/kubestrata/internal/snapshot"
)

// resourceOf maps the fixture kinds to their API resources.
var resourceOf = map[string]APIResource{
	"/Namespace":                         {Version: "v1", Resource: "namespaces", Kind: "Namespace"},
	"/Node":                              {Version: "v1", Resource: "nodes", Kind: "Node"},
	"/Pod":                               {Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true},
	"/Service":                           {Version: "v1", Resource: "services", Kind: "Service", Namespaced: true},
	"/ConfigMap":                         {Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespaced: true},
	"/ServiceAccount":                    {Version: "v1", Resource: "serviceaccounts", Kind: "ServiceAccount", Namespaced: true},
	"/PersistentVolumeClaim":             {Version: "v1", Resource: "persistentvolumeclaims", Kind: "PersistentVolumeClaim", Namespaced: true},
	"/PersistentVolume":                  {Version: "v1", Resource: "persistentvolumes", Kind: "PersistentVolume"},
	"/ReplicationController":             {Version: "v1", Resource: "replicationcontrollers", Kind: "ReplicationController", Namespaced: true},
	"apps/Deployment":                    {Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true},
	"apps/ReplicaSet":                    {Group: "apps", Version: "v1", Resource: "replicasets", Kind: "ReplicaSet", Namespaced: true},
	"networking.k8s.io/Ingress":          {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Kind: "Ingress", Namespaced: true},
	"storage.k8s.io/StorageClass":        {Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses", Kind: "StorageClass"},
	"route.openshift.io/Route":           {Group: "route.openshift.io", Version: "v1", Resource: "routes", Kind: "Route", Namespaced: true, CRD: true},
	"apps.openshift.io/DeploymentConfig": {Group: "apps.openshift.io", Version: "v1", Resource: "deploymentconfigs", Kind: "DeploymentConfig", Namespaced: true, CRD: true},
	"db.example.com/Database":            {Group: "db.example.com", Version: "v1", Resource: "databases", Kind: "Database", Namespaced: true, CRD: true},
}

type fakeSource struct {
	objs    map[schema.GroupResource][]unstructured.Unstructured
	secrets []SecretMeta
}

func loadFake(t *testing.T) *fakeSource {
	t.Helper()
	raw, err := os.ReadFile("testdata/cluster.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSource{objs: map[schema.GroupResource][]unstructured.Unstructured{}}
	for _, doc := range strings.Split(string(raw), "\n---\n") {
		js, err := yaml.YAMLToJSON([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		var u unstructured.Unstructured
		if err := json.Unmarshal(js, &u.Object); err != nil {
			t.Fatal(err)
		}
		gv, _ := schema.ParseGroupVersion(u.GetAPIVersion())
		r, ok := resourceOf[gv.Group+"/"+u.GetKind()]
		if !ok {
			t.Fatalf("fixture kind %s/%s not mapped", gv.Group, u.GetKind())
		}
		gr := schema.GroupResource{Group: r.Group, Resource: r.Resource}
		f.objs[gr] = append(f.objs[gr], u)
	}
	ts := func(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }
	f.secrets = []SecretMeta{
		{Name: "api-db", NS: "payments", Type: "Opaque", Keys: 2, UID: "sec-api-db", Created: ts("2025-01-10T00:00:00Z")},
		{Name: "api-tls", NS: "payments", Type: "kubernetes.io/tls", Keys: 2, UID: "sec-api-tls", Created: ts("2025-01-10T00:00:00Z")},
		{Name: "sh.helm.release.v1.api.v3", NS: "payments", Type: "helm.sh/release.v1", Keys: 1, UID: "sec-helm",
			Labels:  map[string]string{"owner": "helm", "name": "api", "version": "3", "status": "deployed"},
			Created: ts("2026-10-01T00:00:00Z")},
	}
	return f
}

func (f *fakeSource) Server() string { return "https://127.0.0.1:6443" }
func (f *fakeSource) ServerVersion(context.Context) (string, error) {
	return "v1.35.0", nil
}
func (f *fakeSource) KubeSystemUID(context.Context) (string, error) { return "ns-kube-system", nil }
func (f *fakeSource) Discover(context.Context) ([]APIResource, []error) {
	var out []APIResource
	for _, r := range resourceOf {
		out = append(out, r)
	}
	out = append(out, APIResource{Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true})
	return out, nil
}
func (f *fakeSource) List(_ context.Context, r APIResource) ([]unstructured.Unstructured, error) {
	if r.Resource == "secrets" {
		return nil, errors.New("collector must not list full Secrets")
	}
	return f.objs[schema.GroupResource{Group: r.Group, Resource: r.Resource}], nil
}
func (f *fakeSource) ListSecretsMeta(context.Context) ([]SecretMeta, error) { return f.secrets, nil }

var fixedNow = func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }

func collectFixture(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	snap, err := Collect(context.Background(), loadFake(t), Options{Context: "lab", ToolVersion: "test", CRDs: true, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func find(t *testing.T, s *snapshot.Snapshot, id string) snapshot.Resource {
	t.Helper()
	for _, r := range s.Resources {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("resource %s not in snapshot", id)
	return snapshot.Resource{}
}

func hasEdge(s *snapshot.Snapshot, from, to, rel string) bool {
	for _, e := range s.Edges {
		if e.From == from && e.To == to && e.Rel == rel {
			return true
		}
	}
	return false
}

func TestNoSecretMaterialLeaks(t *testing.T) {
	snap := collectFixture(t)
	var buf bytes.Buffer
	if err := snapshot.WriteGzip(&buf, snap); err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(snap)
	if bytes.Contains(plain, []byte("LEAK-")) {
		i := bytes.Index(plain, []byte("LEAK-"))
		t.Fatalf("snapshot leaks seeded value near: %s", plain[max(0, i-80):min(len(plain), i+40)])
	}
	if bytes.Contains(plain, []byte("last-applied-configuration")) {
		t.Fatal("snapshot carries the last-applied annotation")
	}
}

func TestPodsAggregateIntoOwners(t *testing.T) {
	snap := collectFixture(t)
	api := find(t, snap, "apps/Deployment/payments/api")
	if api.Facts["pods"] != int64(2) || api.Facts["ready"] != int64(2) || api.Facts["restarts"] != int64(2) {
		t.Fatalf("aggregation: %v", api.Facts)
	}
	if got := api.Facts["last_rollout"].(time.Time); !got.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("last_rollout = %v", got)
	}
	if archs := api.Facts["archs"].([]string); len(archs) != 1 || archs[0] != "arm64" {
		t.Fatalf("archs = %v", archs)
	}
	dc := find(t, snap, "apps.openshift.io/DeploymentConfig/legacy/web")
	if dc.Facts["pods"] != int64(1) || dc.Facts["end_of_life"] == nil {
		t.Fatalf("DeploymentConfig facts: %v", dc.Facts)
	}
	for _, r := range snap.Resources {
		if r.Kind == "ReplicaSet" || r.Kind == "ReplicationController" || (r.Kind == "Pod" && r.NS == "payments") {
			t.Errorf("support object shown: %s", r.ID)
		}
	}
	bare := find(t, snap, "core/Pod/legacy/bare")
	if bare.Facts["orphan"] != true {
		t.Fatalf("orphan pod facts: %v", bare.Facts)
	}
}

func TestTimesAndManagers(t *testing.T) {
	snap := collectFixture(t)
	api := find(t, snap, "apps/Deployment/payments/api")
	if !api.Changed.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("changed = %v (status updates must be ignored)", api.Changed)
	}
	cases := map[string]string{
		"apps/Deployment/payments/api":                   MgrHelm,
		"apps/Deployment/payments/db-proxy":              MgrOperator,
		"route.openshift.io/Route/legacy/web":            MgrKubectl,
		"core/Secret/payments/sh.helm.release.v1.api.v3": MgrHelm,
		"core/Service/payments/api":                      MgrUnknown,
	}
	for id, want := range cases {
		if got := find(t, snap, id).Manager; got != want {
			t.Errorf("%s manager = %s, want %s", id, got, want)
		}
	}
	if got := find(t, snap, "apps/Deployment/payments/db-proxy").Owner; got != "db.example.com/Database/payments/db" {
		t.Errorf("db-proxy owner = %q", got)
	}
}

func TestEdges(t *testing.T) {
	snap := collectFixture(t)
	want := [][3]string{
		{"networking.k8s.io/Ingress/payments/api", "core/Service/payments/api", RelRoutes},
		{"networking.k8s.io/Ingress/payments/api", "core/Secret/payments/api-tls", RelTLS},
		{"core/Service/payments/api", "apps/Deployment/payments/api", RelSelects},
		{"apps/Deployment/payments/api", "core/PersistentVolumeClaim/payments/data", RelMounts},
		{"apps/Deployment/payments/api", "core/ConfigMap/payments/api-config", RelMounts},
		{"apps/Deployment/payments/api", "core/Secret/payments/api-db", RelEnvFrom},
		{"apps/Deployment/payments/api", "core/ServiceAccount/payments/api", RelUsesSA},
		{"core/PersistentVolumeClaim/payments/data", "core/PersistentVolume/_/pv-1", RelBinds},
		{"core/PersistentVolumeClaim/payments/data", "storage.k8s.io/StorageClass/_/standard", RelBinds},
		{"route.openshift.io/Route/legacy/web", "core/Service/legacy/web", RelRoutes},
		{"core/Service/legacy/web", "apps.openshift.io/DeploymentConfig/legacy/web", RelSelects},
		{"db.example.com/Database/payments/db", "apps/Deployment/payments/db-proxy", RelOwns},
	}
	for _, w := range want {
		if !hasEdge(snap, w[0], w[1], w[2]) {
			t.Errorf("missing edge %s -%s-> %s", w[0], w[2], w[1])
		}
	}
	if find(t, snap, "core/Service/payments/orphan-svc").Facts["selects_none"] != true {
		t.Error("orphan service not flagged")
	}
}

func TestNoiseAndClassification(t *testing.T) {
	snap := collectFixture(t)
	for _, r := range snap.Resources {
		if r.ID == "core/ConfigMap/payments/kube-root-ca.crt" || r.ID == "core/ServiceAccount/payments/default" {
			t.Errorf("noise kept: %s", r.ID)
		}
	}
	if find(t, snap, "networking.k8s.io/Ingress/payments/api").Facts["end_of_life"] == nil {
		t.Error("ingress-nginx not flagged")
	}
	if find(t, snap, "storage.k8s.io/StorageClass/_/standard").Facts["end_of_life"] == nil {
		t.Error("in-tree provisioner not flagged")
	}
	if find(t, snap, "core/ServiceAccount/payments/api").Facts["cloud_identity"] != "aws-irsa" {
		t.Error("IRSA not detected")
	}
	if snap.Cluster.Distro != "openshift" || len(snap.Cluster.DistroSignals) == 0 {
		t.Errorf("distro = %s %v", snap.Cluster.Distro, snap.Cluster.DistroSignals)
	}
	teams := map[string]string{}
	for _, n := range snap.Namespaces {
		teams[n.Name] = n.Team
	}
	if teams["payments"] != "team-payments" || teams["legacy"] != "alice" {
		t.Errorf("namespace teams = %v", teams)
	}
	if find(t, snap, "core/Secret/payments/api-db").Team != "team-payments" {
		t.Error("resource does not inherit the namespace team")
	}
	if snap.Cluster.Nodes.Count != 1 || snap.Cluster.Nodes.Arch["arm64"] != 1 {
		t.Errorf("nodes = %+v", snap.Cluster.Nodes)
	}
}

func TestDeterministic(t *testing.T) {
	a, _ := json.Marshal(collectFixture(t))
	b, _ := json.Marshal(collectFixture(t))
	if !bytes.Equal(a, b) {
		t.Fatal("two collections of the same state differ")
	}
}

func TestChangedIgnoresStatus(t *testing.T) {
	ts := func(s string) *metav1.Time { t, _ := time.Parse(time.RFC3339, s); mt := metav1.NewTime(t); return &mt }
	got := changedAt([]metav1.ManagedFieldsEntry{
		{Manager: "a", Time: ts("2025-01-01T00:00:00Z")},
		{Manager: "b", Subresource: "status", Time: ts("2026-01-01T00:00:00Z")},
	})
	if !got.Equal(ts("2025-01-01T00:00:00Z").Time) {
		t.Fatalf("changedAt = %v", got)
	}
}

func TestDetectDistro(t *testing.T) {
	cases := []struct {
		groups      map[string]bool
		host, ver   string
		providerIDs []string
		want        string
	}{
		{map[string]bool{"route.openshift.io": true}, "127.0.0.1:6443", "v1.35.0", []string{"kind://docker/x/y"}, "openshift"},
		{nil, "ABC.gr7.us-east-1.eks.amazonaws.com", "v1.35.2-eks-1a2b3c", nil, "eks"},
		{nil, "127.0.0.1:26443", "v1.36.5+orb1", nil, "orbstack"},
		{nil, "127.0.0.1:6443", "v1.33.1+k3s1", nil, "k3s"},
		{nil, "127.0.0.1:6443", "v1.35.0", []string{"kind://docker/lab/n1"}, "kind"},
		{nil, "10.0.0.1:6443", "v1.35.0", nil, "kubernetes"},
	}
	for _, c := range cases {
		if got, _ := detectDistro(c.groups, c.host, c.ver, c.providerIDs, nil); got != c.want {
			t.Errorf("%s %s → %s, want %s", c.host, c.ver, got, c.want)
		}
	}
}
