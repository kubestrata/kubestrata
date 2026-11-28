package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/kubestrata/kubestrata/internal/snapshot"
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
	"sparkoperator.k8s.io/SparkApplication": {Group: "sparkoperator.k8s.io", Version: "v1beta2", Resource: "sparkapplications",
		Kind: "SparkApplication", Namespaced: true, CRD: true},
	"karpenter.sh/NodePool":          {Group: "karpenter.sh", Version: "v1", Resource: "nodepools", Kind: "NodePool", CRD: true},
	"karpenter.sh/NodeClaim":         {Group: "karpenter.sh", Version: "v1", Resource: "nodeclaims", Kind: "NodeClaim", CRD: true},
	"eks.amazonaws.com/NodeClass":    {Group: "eks.amazonaws.com", Version: "v1", Resource: "nodeclasses", Kind: "NodeClass", CRD: true},
	"karpenter.k8s.aws/EC2NodeClass": {Group: "karpenter.k8s.aws", Version: "v1", Resource: "ec2nodeclasses", Kind: "EC2NodeClass", CRD: true},
}

type fakeSource struct {
	objs    map[schema.GroupResource][]unstructured.Unstructured
	secrets []SecretMeta
	apis    []APIResource // served resources; nil serves resourceOf and Secrets
	// secretCalls counts ListSecretsMeta calls
	secretCalls atomic.Int32
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
	mf := func(at string, managers ...string) []metav1.ManagedFieldsEntry {
		t := metav1.NewTime(ts(at))
		var out []metav1.ManagedFieldsEntry
		for _, m := range managers {
			out = append(out, metav1.ManagedFieldsEntry{Manager: m, Operation: "Update", Time: &t})
		}
		return out
	}
	f.secrets = []SecretMeta{
		// Terraform created the token Secret, the token controller filled it in the same second
		{Name: "tf-sa-token", NS: "payments", Type: "kubernetes.io/service-account-token", Keys: 3, UID: "sec-tf-token",
			Created: ts("2026-11-10T00:00:00Z"), ManagedFields: mf("2026-11-10T00:00:00Z", "kube-controller-manager", "HashiCorp")},
		// only the control plane wrote it
		{Name: "bootstrap-token-abcdef", NS: "kube-system", Type: "bootstrap.kubernetes.io/token", Keys: 4, UID: "sec-boot",
			Created: ts("2026-11-10T00:00:00Z"), ManagedFields: mf("2026-11-10T00:00:00Z", "kube-controller-manager")},
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
	if f.apis != nil {
		return f.apis, nil
	}
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
func (f *fakeSource) ListSecretsMeta(context.Context) ([]SecretMeta, error) {
	f.secretCalls.Add(1)
	return f.secrets, nil
}

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
		"apps/Deployment/payments/api":                                      MgrHelm,
		"apps/Deployment/payments/db-proxy":                                 MgrOperator,
		"route.openshift.io/Route/legacy/web":                               MgrKubectl,
		"core/Secret/payments/sh.helm.release.v1.api.v3":                    MgrHelm,
		"core/Service/payments/api":                                         MgrUnknown,
		"core/ServiceAccount/payments/tf-runner":                            MgrTerraform,
		"core/Secret/payments/tf-sa-token":                                  MgrTerraform,
		"core/Secret/kube-system/bootstrap-token-abcdef":                    MgrPlatform,
		"core/ConfigMap/kube-system/aws-auth":                               MgrPlatform,
		"karpenter.sh/NodePool/_/general-purpose":                           MgrTerraform,
		"karpenter.sh/NodeClaim/_/general-purpose-abcde":                    MgrOperator,
		"eks.amazonaws.com/NodeClass/_/default":                             MgrPlatform,
		"karpenter.k8s.aws/EC2NodeClass/_/default":                          MgrTerraform,
		"sparkoperator.k8s.io/SparkApplication/spark-team-a/order-ondemand": MgrKubectl,
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
	from := map[string]string{}
	for _, n := range snap.Namespaces {
		from[n.Name] = n.TeamFrom
	}
	if from["payments"] != "team" || from["legacy"] != TeamFromRequester {
		t.Errorf("namespace team sources = %v", from)
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

func TestSystemNamespaces(t *testing.T) {
	re := regexp.MustCompile(DefaultSystemNS)
	for ns, want := range map[string]bool{
		"kube-system": true, "kube-public": true, "kube-node-lease": true, "local-path-storage": true,
		"openshift": true, "openshift-ingress": true, "amazon-cloudwatch": true, "aws-observability": true,
		"gke-managed-system": true, "azure-arc": true,
		"kube-prometheus-stack": false, "kube-system-extra": false, "openshiftx": false, "payments": false,
		"spark-team-a": false, "my-amazon-app": false, "local-path-storage-2": false,
	} {
		if got := re.MatchString(ns); got != want {
			t.Errorf("%s: system %v, want %v", ns, got, want)
		}
	}
	snap := collectFixture(t)
	sys := map[string]bool{}
	for _, n := range snap.Namespaces {
		sys[n.Name] = n.System
	}
	if !sys["kube-system"] || !sys["amazon-cloudwatch"] || sys["kube-prometheus-stack"] || sys["payments"] {
		t.Errorf("namespace system flags: %v", sys)
	}
}

// Pods owned by a custom resource fold into it like into a Deployment: the operator owns the
// driver pod, the driver owns the executor pods (K2).
func TestPodsAggregateIntoCustomResource(t *testing.T) {
	snap := collectFixture(t)
	app := find(t, snap, "sparkoperator.k8s.io/SparkApplication/spark-team-a/order-ondemand")
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	f := app.Facts
	if f["pods"] != int64(3) || f["ready"] != int64(2) || f["restarts"] != int64(3) {
		t.Fatalf("pods/ready/restarts: %v", f)
	}
	if archs, _ := f["archs"].([]string); len(archs) != 1 || archs[0] != "arm64" {
		t.Errorf("archs = %v", f["archs"])
	}
	if got, _ := f["pod_oldest"].(time.Time); !got.Equal(at("2026-11-16T10:00:05Z")) {
		t.Errorf("pod_oldest = %v", f["pod_oldest"])
	}
	if got, _ := f["pod_newest"].(time.Time); !got.Equal(at("2026-11-16T10:00:31Z")) {
		t.Errorf("pod_newest = %v", f["pod_newest"])
	}
	for _, r := range snap.Resources {
		if r.Kind == "Pod" && r.NS == "spark-team-a" {
			t.Errorf("Spark pod shown on its own: %s", r.ID)
		}
	}
}

// Karpenter and EKS Auto Mode facts come from labels and spec, never from status (K4).
func TestNodeProvisioningFacts(t *testing.T) {
	snap := collectFixture(t)
	str := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	want := map[string]map[string]string{
		"karpenter.sh/NodeClaim/_/general-purpose-abcde": {
			"instance_type": `"m7g.large"`, "capacity_type": `"on-demand"`, "zone": `"zone-a"`,
			"nodepool": `"general-purpose"`, "node_class": `"eks.amazonaws.com/NodeClass/default"`, "node": `"node1"`},
		// no labels yet: single-valued requirements fill in, a requirement with two zones does not
		"karpenter.sh/NodeClaim/_/general-purpose-fghij": {
			"instance_type": `"c6a.xlarge"`, "capacity_type": `"spot"`, "nodepool": `"general-purpose"`},
		"karpenter.sh/NodePool/_/general-purpose": {
			"requirements": `["eks.amazonaws.com/instance-category In c,m","karpenter.sh/capacity-type In on-demand,spot","kubernetes.io/arch Exists"]`,
			"limits":       `["cpu=1000","memory=1000Gi"]`, "weight": `10`, "node_class": `"eks.amazonaws.com/NodeClass/default"`,
			"expire_after": `"336h"`, "consolidation_policy": `"WhenEmptyOrUnderutilized"`, "consolidate_after": `"30s"`,
			"disruption_budgets": `["nodes=10%","nodes=0 reasons=Drifted,Underutilized schedule=0 9 * * mon-fri for 8h"]`},
		"eks.amazonaws.com/NodeClass/_/default": {
			"ephemeral_storage": `{"iops":3000,"size":"80Gi","throughput":125}`},
		"karpenter.k8s.aws/EC2NodeClass/_/default": {
			"block_devices": `[{"device":"/dev/xvda","iops":6000,"size":"100Gi","throughput":1000,"type":"gp3"}]`},
	}
	for id, facts := range want {
		r := find(t, snap, id)
		for k, v := range facts {
			if got := str(r.Facts[k]); got != v {
				t.Errorf("%s %s = %s, want %s", id, k, got, v)
			}
		}
	}
	if _, ok := find(t, snap, "karpenter.sh/NodeClaim/_/general-purpose-fghij").Facts["zone"]; ok {
		t.Error("zone set from a requirement with two values")
	}
	if !hasEdge(snap, "karpenter.sh/NodePool/_/general-purpose", "karpenter.sh/NodeClaim/_/general-purpose-abcde", RelOwns) {
		t.Error("NodePool does not own its NodeClaim")
	}
	plain, _ := json.Marshal(snap)
	if bytes.Contains(plain, []byte("STATUS-")) {
		t.Error("NodePool status reached the snapshot")
	}
	// the node's status (node name) stays out of the fingerprint; spec facts are in it
	nc := find(t, snap, "karpenter.sh/NodeClaim/_/general-purpose-abcde")
	moved := map[string]any{}
	for k, v := range nc.Facts {
		moved[k] = v
	}
	moved["node"] = "node2"
	if fingerprint(nc.Kind, moved) != nc.FP {
		t.Error("the NodeClaim node name changes the fingerprint")
	}
	moved["instance_type"] = "m7g.xlarge"
	if fingerprint(nc.Kind, moved) == nc.FP {
		t.Error("the NodeClaim instance type is not in the fingerprint")
	}
	n := snap.Cluster.Nodes
	if n.InstanceTypes["m7g.large"] != 1 || n.CapacityTypes["on-demand"] != 1 {
		t.Errorf("node summary: %+v", n)
	}
}

func TestCapacityType(t *testing.T) {
	for _, c := range []struct {
		labels map[string]string
		want   string
	}{
		{map[string]string{"karpenter.sh/capacity-type": "spot"}, "spot"},
		{map[string]string{"eks.amazonaws.com/capacityType": "ON_DEMAND"}, "on-demand"},
		{map[string]string{"eks.amazonaws.com/capacityType": "CAPACITY_BLOCK"}, "capacity-block"},
		{map[string]string{"cloud.google.com/gke-spot": "true"}, "spot"},
		{map[string]string{"cloud.google.com/gke-preemptible": "true"}, "preemptible"},
		{map[string]string{"kubernetes.azure.com/scalesetpriority": "spot"}, "spot"},
		{map[string]string{"kubernetes.io/arch": "amd64"}, ""},
	} {
		if got := capacityType(c.labels); got != c.want {
			t.Errorf("%v → %q, want %q", c.labels, got, c.want)
		}
	}
}

func TestManagerVocabulary(t *testing.T) {
	ts := func(s string) *metav1.Time { v, _ := time.Parse(time.RFC3339, s); mt := metav1.NewTime(v); return &mt }
	t1, t2 := ts("2026-11-10T00:00:00Z"), ts("2026-11-10T00:05:00Z")
	e := func(m string, at *metav1.Time) metav1.ManagedFieldsEntry {
		return metav1.ManagedFieldsEntry{Manager: m, Operation: "Update", Time: at}
	}
	cases := []struct {
		name   string
		labels map[string]string
		mf     []metav1.ManagedFieldsEntry
		ownCR  bool
		want   string
	}{
		{"terraform kubernetes provider", nil, []metav1.ManagedFieldsEntry{e("HashiCorp", t1)}, false, MgrTerraform},
		{"terraform server-side apply", nil, []metav1.ManagedFieldsEntry{e("Terraform", t1)}, false, MgrTerraform},
		{"terraform helm provider", nil, []metav1.ManagedFieldsEntry{e("terraform-provider-helm_v2.17.0_x5", t1)}, false, MgrTerraform},
		{"terraform, then Auto Mode", nil, []metav1.ManagedFieldsEntry{e("HashiCorp", t1), e("eks-auto-mode", t2)}, false, MgrTerraform},
		{"Auto Mode tie goes to terraform", nil, []metav1.ManagedFieldsEntry{e("eks-auto-mode", t1), e("HashiCorp", t1)}, false, MgrTerraform},
		{"eks", nil, []metav1.ManagedFieldsEntry{e("eks", t1)}, false, MgrPlatform},
		{"eks-internal", nil, []metav1.ManagedFieldsEntry{e("eks-internal", t1)}, false, MgrPlatform},
		{"eks-auto-mode only", nil, []metav1.ManagedFieldsEntry{e("eks-auto-mode", t1)}, false, MgrPlatform},
		{"kube-apiserver only", nil, []metav1.ManagedFieldsEntry{e("kube-apiserver", t1)}, false, MgrPlatform},
		{"kubeadm bootstrap", nil, []metav1.ManagedFieldsEntry{e("kubeadm", t1)}, false, MgrPlatform},
		{"kube-controller-manager only", nil, []metav1.ManagedFieldsEntry{e("kube-controller-manager", t1)}, false, MgrPlatform},
		{"kube-controller-manager after kubectl", nil,
			[]metav1.ManagedFieldsEntry{e("kube-controller-manager", t1), e("kubectl-create", t2)}, false, MgrKubectl},
		{"eks, then kubectl edit", nil, []metav1.ManagedFieldsEntry{e("eks", t1), e("kubectl-edit", t2)}, false, MgrPlatform},
		{"helm label wins over terraform", map[string]string{"app.kubernetes.io/managed-by": "Helm"},
			[]metav1.ManagedFieldsEntry{e("terraform-provider-helm_v2.17.0_x5", t1)}, false, MgrHelm},
		{"argocd label wins over eks", map[string]string{"argocd.argoproj.io/instance": "x"},
			[]metav1.ManagedFieldsEntry{e("eks", t1)}, false, MgrArgoCD},
		{"custom resource owner wins over platform", nil, []metav1.ManagedFieldsEntry{e("eks-auto-mode", t1)}, true, MgrOperator},
		{"unknown tool", nil, []metav1.ManagedFieldsEntry{e("pulumi-kubernetes", t1)}, false, MgrOther},
		{"kapp", nil, []metav1.ManagedFieldsEntry{e("kapp", t1)}, false, MgrKapp},
		{"kapp-controller", nil, []metav1.ManagedFieldsEntry{e("kapp-controller", t1)}, false, MgrKapp},
		{"kapp label wins over helm", map[string]string{"kapp.k14s.io/app": "1690000000", "app.kubernetes.io/managed-by": "Helm"},
			[]metav1.ManagedFieldsEntry{e("kapp", t1)}, false, MgrKapp},
		{"kappa is not kapp", nil, []metav1.ManagedFieldsEntry{e("kappa", t1)}, false, MgrOther},
		{"web console", nil, []metav1.ManagedFieldsEntry{e("Mozilla", t1)}, false, MgrConsole},
		{"created by kubectl, edited in the console", nil, []metav1.ManagedFieldsEntry{e("kubectl-create", t1), e("Mozilla", t2)},
			false, MgrKubectl},
		{"created in the console, then kubectl", nil, []metav1.ManagedFieldsEntry{e("Mozilla", t1), e("kubectl-edit", t2)},
			false, MgrConsole},
		{"no data", nil, nil, false, MgrUnknown},
	}
	for _, c := range cases {
		if got, _ := classifyManager(c.labels, nil, c.mf, c.ownCR); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// manager is a resource field and field_managers a volatile fact: neither enters the
// fingerprint, so a move from kubectl to Terraform is not a drift.
func TestFingerprintIgnoresManagers(t *testing.T) {
	facts := map[string]any{"images": []string{"a:1"}, "field_managers": []string{"kubectl-client-side-apply"}}
	other := map[string]any{"images": []string{"a:1"}, "field_managers": []string{"HashiCorp", "eks-auto-mode"}}
	if fingerprint("Deployment", facts) != fingerprint("Deployment", other) {
		t.Error("field_managers changes the fingerprint")
	}
	if !Volatile("field_managers") {
		t.Error("field_managers is not volatile")
	}
	snap := collectFixture(t)
	sa := find(t, snap, "core/ServiceAccount/payments/tf-runner")
	if sa.FP != fingerprint(sa.Kind, sa.Facts) {
		t.Error("fingerprint depends on more than the kind and the facts")
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

func TestCreatorSkipsSystemManagers(t *testing.T) {
	ts := func(s string) *metav1.Time { t, _ := time.Parse(time.RFC3339, s); mt := metav1.NewTime(t); return &mt }
	same := ts("2025-01-01T00:00:00Z")
	mf := []metav1.ManagedFieldsEntry{
		{Manager: "kube-controller-manager", Operation: "Update", Time: same},
		{Manager: "kubectl-client-side-apply", Operation: "Update", Time: same},
	}
	if got := creatorManager(mf); got != "kubectl-client-side-apply" {
		t.Fatalf("creator = %s", got)
	}
	if got := creatorManager(mf[:1]); got != "kube-controller-manager" {
		t.Fatalf("only system manager: creator = %s", got)
	}
}

// obj builds an unstructured object created at 2026-01-01 by one field manager.
func obj(apiVersion, kind, ns, name, manager string, labels map[string]string, fields map[string]any) unstructured.Unstructured {
	meta := map[string]any{"name": name, "uid": "uid-" + ns + "-" + kind + "-" + name,
		"creationTimestamp": "2026-01-01T00:00:00Z",
		"managedFields":     []any{map[string]any{"manager": manager, "operation": "Update", "time": "2026-01-01T00:00:00Z"}}}
	if ns != "" {
		meta["namespace"] = ns
	}
	if labels != nil {
		l := map[string]any{}
		for k, v := range labels {
			l[k] = v
		}
		meta["labels"] = l
	}
	o := map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": meta}
	for k, v := range fields {
		o[k] = v
	}
	return unstructured.Unstructured{Object: o}
}

var applicationAPI = APIResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications", Kind: "Application",
	Namespaced: true, CRD: true}

// smallSource serves namespaces and the given resources with their objects.
func smallSource(objs map[APIResource][]unstructured.Unstructured) *fakeSource {
	f := &fakeSource{objs: map[schema.GroupResource][]unstructured.Unstructured{}}
	ns := resourceOf["/Namespace"]
	f.apis = []APIResource{ns}
	f.objs[schema.GroupResource{Resource: "namespaces"}] = []unstructured.Unstructured{
		obj("v1", "Namespace", "", "ns-a", "kubectl-create", nil, nil),
		obj("v1", "Namespace", "", "openshift-gitops", "kubectl-create", nil, nil)}
	for r, list := range objs {
		f.apis = append(f.apis, r)
		f.objs[schema.GroupResource{Group: r.Group, Resource: r.Resource}] = list
	}
	return f
}

func application(name, server string, managed ...map[string]any) unstructured.Unstructured {
	var res []any
	for _, r := range managed {
		res = append(res, r)
	}
	return obj("argoproj.io/v1alpha1", "Application", "openshift-gitops", name, "kubectl-client-side-apply", nil,
		map[string]any{"spec": map[string]any{"destination": map[string]any{"server": server, "namespace": "ns-a"}},
			"status": map[string]any{"resources": res}})
}

// Argo CD objects tracked by neither label nor annotation are attributed through the
// status.resources of the Applications that deploy to this cluster (K14).
func TestArgoCDFromApplicationStatus(t *testing.T) {
	deploy, svc, sc := resourceOf["apps/Deployment"], resourceOf["/Service"], resourceOf["storage.k8s.io/StorageClass"]
	src := smallSource(map[APIResource][]unstructured.Unstructured{
		deploy: {obj("apps/v1", "Deployment", "ns-a", "web", "kubectl-client-side-apply", nil, nil),
			obj("apps/v1", "Deployment", "ns-a", "other", "kubectl-client-side-apply", nil, nil)},
		svc: {obj("v1", "Service", "ns-a", "web", "argocd-controller", map[string]string{"argocd.argoproj.io/instance": "web"},
			map[string]any{"spec": map[string]any{"ports": []any{map[string]any{"port": int64(80)}}}})},
		sc: {obj("storage.k8s.io/v1", "StorageClass", "", "fast", "kubectl-create", nil, map[string]any{"provisioner": "csi.example.com"})},
		applicationAPI: {
			application("web", "https://kubernetes.default.svc",
				map[string]any{"group": "apps", "kind": "Deployment", "namespace": "ns-a", "name": "web"},
				map[string]any{"kind": "Service", "namespace": "ns-a", "name": "web"},
				map[string]any{"group": "storage.k8s.io", "kind": "StorageClass", "name": "fast"},
				map[string]any{"group": "apps", "kind": "Deployment", "namespace": "ns-a", "name": "not-collected"}),
			// a remote cluster's objects share names with local ones: not attributed
			application("remote", "https://api.remote.example.com:6443",
				map[string]any{"group": "apps", "kind": "Deployment", "namespace": "ns-a", "name": "other"}),
		},
	})
	snap, err := Collect(context.Background(), src, Options{Context: "lab", ToolVersion: "test", CRDs: true, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"apps/Deployment/ns-a/web":                     MgrArgoCD,
		"core/Service/ns-a/web":                        MgrArgoCD,
		"storage.k8s.io/StorageClass/_/fast":           MgrArgoCD,
		"apps/Deployment/ns-a/other":                   MgrKubectl,
		"argoproj.io/Application/openshift-gitops/web": MgrKubectl,
	} {
		if got := find(t, snap, id).Manager; got != want {
			t.Errorf("%s manager = %s, want %s", id, got, want)
		}
	}
	want := snapshot.ArgoCD{Applications: 2, Remote: 1, Managed: 3, FromStatus: 2}
	if a := snap.Cluster.ArgoCD; a == nil || *a != want {
		t.Errorf("argocd = %+v, want %+v", a, want)
	}
}

// With the Application kind served and nothing attributed, the snapshot says so; without the
// kind, it says nothing.
func TestArgoCDInstalledNothingManaged(t *testing.T) {
	src := smallSource(map[APIResource][]unstructured.Unstructured{
		resourceOf["apps/Deployment"]: {obj("apps/v1", "Deployment", "ns-a", "web", "kubectl-client-side-apply", nil, nil)},
		applicationAPI:                nil,
	})
	snap, err := Collect(context.Background(), src, Options{Context: "lab", ToolVersion: "test", CRDs: true, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if a := snap.Cluster.ArgoCD; a == nil || *a != (snapshot.ArgoCD{}) {
		t.Errorf("argocd = %+v, want installed with nothing managed", a)
	}
	if a := collectFixture(t).Cluster.ArgoCD; a != nil {
		t.Errorf("fixture without Argo CD: argocd = %+v", a)
	}
}

func TestLocalDestination(t *testing.T) {
	for dest, want := range map[[2]string]bool{
		{"https://kubernetes.default.svc", ""}:      true,
		{"https://kubernetes.default.svc/", ""}:     true,
		{"", "in-cluster"}:                          true,
		{"", ""}:                                    true,
		{"https://api.remote.example.com:6443", ""}: false,
		{"", "remote"}:                              false,
	} {
		if got := localDestination(map[string]any{"server": dest[0], "name": dest[1]}); got != want {
			t.Errorf("%v: local %v, want %v", dest, got, want)
		}
	}
}

func TestSkipMatcher(t *testing.T) {
	m, err := SkipMatcher([]string{"core/Secret", "route.openshift.io/Route", "machineconfig", " Pod "})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		group, kind string
		want        bool
	}{
		{"", "Secret", true}, {"example.com", "Secret", false}, {"route.openshift.io", "Route", true},
		{"gateway.networking.k8s.io", "HTTPRoute", false}, {"machineconfiguration.openshift.io", "MachineConfig", true},
		{"", "Pod", true}, {"", "ConfigMap", false}, {"Route.OpenShift.io", "route", true},
	} {
		if got := m(c.group, c.kind); got != c.want {
			t.Errorf("%s/%s: skipped %v, want %v", c.group, c.kind, got, c.want)
		}
	}
	for _, bad := range [][]string{{""}, {"a/b/c"}, {"core/"}, {"route.openshift.io/ Route"}} {
		if _, err := SkipMatcher(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// Without Secrets the inventory runs with a role that cannot list them: no Secret items, no
// ListSecretsMeta call, references to Secrets are not reported as missing, and Helm is still
// detected from the labels of the objects (K20).
func TestSkipSecrets(t *testing.T) {
	src := loadFake(t)
	snap, err := Collect(context.Background(), src, Options{Context: "lab", ToolVersion: "test", CRDs: true, Now: fixedNow,
		SkipKinds: []string{"core/Secret"}})
	if err != nil {
		t.Fatal(err)
	}
	if n := src.secretCalls.Load(); n != 0 {
		t.Errorf("ListSecretsMeta called %d times", n)
	}
	for _, r := range snap.Resources {
		if r.Kind == "Secret" {
			t.Errorf("Secret collected: %s", r.ID)
		}
		for _, ref := range toStrings(r.Facts["missing_refs"]) {
			if strings.HasPrefix(ref, "Secret/") {
				t.Errorf("%s: skipped Secret reported missing: %s", r.ID, ref)
			}
		}
	}
	if got := snap.Scope.SkippedKinds; len(got) != 1 || got[0] != "core/Secret" {
		t.Errorf("skipped kinds = %v", got)
	}
	for _, k := range snap.Scope.Kinds {
		if k == "core/Secret" {
			t.Error("core/Secret listed as collected")
		}
	}
	if len(snap.Errors) != 0 {
		t.Errorf("errors: %+v", snap.Errors)
	}
	if m := find(t, snap, "apps/Deployment/payments/api").Manager; m != MgrHelm {
		t.Errorf("payments/api manager without Secrets = %s, want helm from its labels", m)
	}
	if hasEdge(snap, "apps/Deployment/payments/api", "core/Secret/payments/api-db", RelEnvFrom) {
		t.Error("edge to a skipped Secret")
	}
	// a skipped custom kind is left out the same way
	snap, err = Collect(context.Background(), loadFake(t), Options{Context: "lab", ToolVersion: "test", CRDs: true, Now: fixedNow,
		SkipKinds: []string{"route.openshift.io/Route"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range snap.Resources {
		if r.Kind == "Route" {
			t.Errorf("Route collected: %s", r.ID)
		}
	}
	if got := snap.Scope.SkippedKinds; len(got) != 1 || got[0] != "route.openshift.io/Route" {
		t.Errorf("skipped kinds = %v", got)
	}
	if _, err := Collect(context.Background(), loadFake(t), Options{SkipKinds: []string{"a/b/c"}}); err == nil {
		t.Error("bad --skip-kinds entry accepted")
	}
}

// An unknown tool keeps its raw field manager name (manager_raw), volatile, for the page's list
// of what "other" holds (K15).
func TestManagerRaw(t *testing.T) {
	src := smallSource(map[APIResource][]unstructured.Unstructured{
		resourceOf["apps/Deployment"]: {obj("apps/v1", "Deployment", "ns-a", "web", "pulumi-kubernetes", nil, nil),
			obj("apps/v1", "Deployment", "ns-a", "api", "kubectl-client-side-apply", nil, nil)},
	})
	snap, err := Collect(context.Background(), src, Options{Context: "lab", ToolVersion: "test", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if web := find(t, snap, "apps/Deployment/ns-a/web"); web.Manager != MgrOther || web.Facts["manager_raw"] != "pulumi-kubernetes" {
		t.Errorf("web: %s %v", web.Manager, web.Facts)
	}
	if api := find(t, snap, "apps/Deployment/ns-a/api"); api.Facts["manager_raw"] != nil {
		t.Errorf("manager_raw on a known manager: %v", api.Facts)
	}
	if !Volatile("manager_raw") {
		t.Error("manager_raw enters the fingerprint")
	}
}
