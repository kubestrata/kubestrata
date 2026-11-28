package collect

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kubestrata/kubestrata/internal/snapshot"
)

// Edge relations.
const (
	RelRoutes     = "routes"
	RelSelects    = "selects"
	RelMounts     = "mounts"
	RelEnvFrom    = "env-from"
	RelUsesSA     = "uses-sa"
	RelPullSecret = "pull-secret"
	RelOwns       = "owns"
	RelBinds      = "binds"
	RelTLS        = "tls"
	RelScales     = "scales"
	RelGrants     = "grants"
	RelAttaches   = "attaches"
)

type ref struct{ group, kind, ns, name, rel string }

// backend is a Service an Ingress, HTTPRoute or Route sends traffic to. Port is as written: the
// Service port (number or name) for an Ingress or HTTPRoute, the target port for a Route.
type backend struct {
	svc, port string
	target    bool // port is a Route targetPort
}

// svcPort is one port of a Service, kept to resolve backend ports.
type svcPort struct {
	name, target string
	port         int64
}

// item is a resource being built, with the data needed for owners and edges that does not go
// into the snapshot.
type item struct {
	res        snapshot.Resource
	uid        string
	ownerUID   string
	tmplLabels map[string]string
	selector   map[string]string
	refs       []ref
	backends   []backend
	ports      []svcPort
}

// servicePort resolves a backend port to the Service port number when the Service is known: by
// port name or number for an Ingress or HTTPRoute, by port name or target port for a Route.
// Otherwise the port stays as written.
func servicePort(ports []svcPort, b backend) string {
	if b.port == "" {
		return ""
	}
	for _, p := range ports {
		if p.name != "" && p.name == b.port || !b.target && itoa(p.port) == b.port || b.target && p.target == b.port {
			return itoa(p.port)
		}
	}
	return b.port
}

func m(v any) map[string]any {
	x, _ := v.(map[string]any)
	return x
}

func s(v any) string {
	x, _ := v.(string)
	return x
}

func sl(v any) []any {
	x, _ := v.([]any)
	return x
}

func path(obj map[string]any, keys ...string) any {
	var cur any = obj
	for _, k := range keys {
		mm := m(cur)
		if mm == nil {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func num(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case int:
		return int64(x), true
	}
	return 0, false
}

func uniqSorted(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range in {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func strMap(v any) map[string]string {
	mm := m(v)
	if len(mm) == 0 {
		return nil
	}
	out := make(map[string]string, len(mm))
	for k, val := range mm {
		out[k] = s(val)
	}
	return out
}

// podSpecFacts records images, sizing, references and risk flags of a pod spec. Env values,
// args and commands are never read.
func podSpecFacts(spec map[string]any, ns string, facts map[string]any, refs *[]ref) {
	var images []string
	var cpuReq, cpuLim, memReq, memLim int64
	privileged := false
	addRef := func(kind, name, rel string) {
		if name != "" {
			*refs = append(*refs, ref{"", kind, ns, name, rel})
		}
	}
	for _, list := range []string{"initContainers", "containers"} {
		for _, c := range sl(spec[list]) {
			cm := m(c)
			images = append(images, s(cm["image"]))
			if list == "containers" {
				res := m(cm["resources"])
				cpuReq += milli(path(res, "requests", "cpu"))
				cpuLim += milli(path(res, "limits", "cpu"))
				memReq += bytesOf(path(res, "requests", "memory"))
				memLim += bytesOf(path(res, "limits", "memory"))
			}
			if b, _ := path(cm, "securityContext", "privileged").(bool); b {
				privileged = true
			}
			for _, e := range sl(cm["env"]) {
				vf := m(m(e)["valueFrom"])
				addRef("Secret", s(path(vf, "secretKeyRef", "name")), RelEnvFrom)
				addRef("ConfigMap", s(path(vf, "configMapKeyRef", "name")), RelEnvFrom)
			}
			for _, e := range sl(cm["envFrom"]) {
				addRef("Secret", s(path(m(e), "secretRef", "name")), RelEnvFrom)
				addRef("ConfigMap", s(path(m(e), "configMapRef", "name")), RelEnvFrom)
			}
		}
	}
	var pvcs []string
	hostPath := false
	for _, v := range sl(spec["volumes"]) {
		vm := m(v)
		if c := s(path(vm, "persistentVolumeClaim", "claimName")); c != "" {
			pvcs = append(pvcs, c)
			addRef("PersistentVolumeClaim", c, RelMounts)
		}
		addRef("Secret", s(path(vm, "secret", "secretName")), RelMounts)
		addRef("ConfigMap", s(path(vm, "configMap", "name")), RelMounts)
		for _, src := range sl(path(vm, "projected", "sources")) {
			addRef("Secret", s(path(m(src), "secret", "name")), RelMounts)
			addRef("ConfigMap", s(path(m(src), "configMap", "name")), RelMounts)
		}
		if vm["hostPath"] != nil {
			hostPath = true
		}
	}
	for _, p := range sl(spec["imagePullSecrets"]) {
		addRef("Secret", s(m(p)["name"]), RelPullSecret)
	}
	sa := s(spec["serviceAccountName"])
	if sa == "" {
		sa = "default"
	}
	addRef("ServiceAccount", sa, RelUsesSA)

	facts["images"] = uniqSorted(images)
	facts["service_account"] = sa
	if len(pvcs) > 0 {
		facts["pvcs"] = uniqSorted(pvcs)
	}
	if cpuReq > 0 {
		facts["cpu_requests_m"] = cpuReq
	}
	if cpuLim > 0 {
		facts["cpu_limits_m"] = cpuLim
	}
	if memReq > 0 {
		facts["mem_requests_b"] = memReq
	}
	if memLim > 0 {
		facts["mem_limits_b"] = memLim
	}
	if ns := m(spec["nodeSelector"]); len(ns) > 0 {
		keys := make([]string, 0, len(ns))
		for k := range ns {
			keys = append(keys, k)
		}
		facts["node_selector"] = uniqSorted(keys)
	}
	if t := sl(spec["tolerations"]); len(t) > 0 {
		facts["tolerations"] = len(t)
	}
	if hostPath {
		facts["host_path"] = true
	}
	if privileged {
		facts["privileged"] = true
	}
	if b, _ := spec["hostNetwork"].(bool); b {
		facts["host_network"] = true
	}
}

func milli(v any) int64 {
	q, ok := quantity(v)
	if !ok {
		return 0
	}
	return q.MilliValue()
}

func bytesOf(v any) int64 {
	q, ok := quantity(v)
	if !ok {
		return 0
	}
	return q.Value()
}

func quantity(v any) (resource.Quantity, bool) {
	switch x := v.(type) {
	case string:
		q, err := resource.ParseQuantity(x)
		return q, err == nil
	case int64:
		return *resource.NewQuantity(x, resource.DecimalSI), true
	case float64:
		return *resource.NewMilliQuantity(int64(x*1000), resource.DecimalSI), true
	}
	return resource.Quantity{}, false
}

// templateOf returns the pod template (metadata + spec) of a workload kind.
func templateOf(kind string, obj map[string]any) map[string]any {
	switch kind {
	case "Pod":
		return obj
	case "CronJob":
		return m(path(obj, "spec", "jobTemplate", "spec", "template"))
	default:
		return m(path(obj, "spec", "template"))
	}
}

// kindFacts fills the facts of one object. It returns false when the object is noise.
func kindFacts(it *item, u *unstructured.Unstructured) bool {
	obj := u.Object
	f := it.res.Facts
	ns := it.res.NS
	spec := m(obj["spec"])
	status := m(obj["status"])
	ann := u.GetAnnotations()
	switch it.res.Kind {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController", "Job", "CronJob",
		"DeploymentConfig", "Pod":
		if tpl := templateOf(it.res.Kind, obj); tpl != nil {
			it.tmplLabels = strMap(path(tpl, "metadata", "labels"))
			podSpecFacts(m(tpl["spec"]), ns, f, &it.refs)
		}
		if r, ok := num(spec["replicas"]); ok {
			f["replicas"] = r
		}
		switch it.res.Kind {
		case "StatefulSet":
			var tpls []map[string]any
			for _, t := range sl(spec["volumeClaimTemplates"]) {
				tm := m(t)
				tpls = append(tpls, map[string]any{
					"name":          s(path(tm, "metadata", "name")),
					"storage_class": s(path(tm, "spec", "storageClassName")),
					"size":          s(path(tm, "spec", "resources", "requests", "storage")),
				})
			}
			if len(tpls) > 0 {
				f["volume_claim_templates"] = tpls
				f["stateful"] = true
			}
		case "CronJob":
			f["schedule"] = s(spec["schedule"])
			if b, _ := spec["suspend"].(bool); b {
				f["suspend"] = true
			}
		case "DeploymentConfig":
			var trig []string
			for _, t := range sl(spec["triggers"]) {
				trig = append(trig, s(m(t)["type"]))
			}
			if len(trig) > 0 {
				f["triggers"] = uniqSorted(trig)
			}
			if st := s(path(spec, "strategy", "type")); st != "" {
				f["strategy"] = st
			}
			f["end_of_life"] = "DeploymentConfig is deprecated since OpenShift 4.14; target: Deployment"
		case "Pod":
			f["phase"] = s(status["phase"])
			f["node"] = s(spec["nodeName"])
		}
		if _, ok := f["pvcs"]; ok {
			f["stateful"] = true
		}
	case "Service":
		if ns == "default" && it.res.Name == "kubernetes" {
			return false // the API server's own Service, recreated by every control plane
		}
		t := s(spec["type"])
		if t == "" {
			t = "ClusterIP"
		}
		f["type"] = t
		var ports []string
		for _, p := range sl(spec["ports"]) {
			pm := m(p)
			port, _ := num(pm["port"])
			proto := s(pm["protocol"])
			if proto == "" {
				proto = "TCP"
			}
			ports = append(ports, itoa(port)+"/"+proto)
		}
		f["ports"] = uniqSorted(ports)
		for _, p := range sl(spec["ports"]) {
			pm := m(p)
			port, _ := num(pm["port"])
			it.ports = append(it.ports, svcPort{name: s(pm["name"]), target: anyString(pm["targetPort"]), port: port})
		}
		if sel := strMap(spec["selector"]); len(sel) > 0 {
			it.selector = sel
			f["selector"] = sel
		}
		if s(spec["clusterIP"]) == "None" {
			f["headless"] = true
		}
		if en := s(spec["externalName"]); en != "" {
			f["external_name"] = en
		}
	case "Ingress":
		class := s(spec["ingressClassName"])
		if class == "" {
			class = ann["kubernetes.io/ingress.class"]
		}
		f["class"] = class
		var hosts, paths []string
		addBackend := func(svc map[string]any) {
			if name := s(svc["name"]); name != "" {
				port := anyString(path(svc, "port", "number"))
				if port == "" || port == "0" {
					port = s(path(svc, "port", "name"))
				}
				it.refs = append(it.refs, ref{"", "Service", ns, name, RelRoutes})
				it.backends = append(it.backends, backend{svc: name, port: port})
			}
		}
		for _, r := range sl(spec["rules"]) {
			rm := m(r)
			hosts = append(hosts, s(rm["host"]))
			for _, p := range sl(path(rm, "http", "paths")) {
				pth := s(m(p)["path"])
				if pth == "" {
					pth = "/"
				}
				paths = append(paths, pth)
				addBackend(m(path(m(p), "backend", "service")))
			}
		}
		if def := m(path(spec, "defaultBackend", "service")); def != nil {
			addBackend(def)
			if len(paths) == 0 {
				paths = append(paths, "/")
			}
		}
		for _, t := range sl(spec["tls"]) {
			it.refs = append(it.refs, ref{"", "Secret", ns, s(m(t)["secretName"]), RelTLS})
		}
		f["hosts"] = uniqSorted(hosts)
		if len(paths) > 0 {
			f["paths"] = uniqSorted(paths)
		}
		f["tls"] = len(sl(spec["tls"])) > 0
		if class == "nginx" {
			f["end_of_life"] = "ingress-nginx was retired in March 2026; target: Gateway API"
		}
	case "Route":
		// spec.tls may embed a private key: only the termination type is kept.
		f["host"] = s(spec["host"])
		if p := s(spec["path"]); p != "" {
			f["path"] = p
		}
		if term := s(path(spec, "tls", "termination")); term != "" {
			f["tls_termination"] = term
		}
		port := anyString(path(spec, "port", "targetPort"))
		for _, b := range append([]any{spec["to"]}, sl(spec["alternateBackends"])...) {
			if name := s(m(b)["name"]); name != "" {
				it.refs = append(it.refs, ref{"", "Service", ns, name, RelRoutes})
				it.backends = append(it.backends, backend{svc: name, port: port, target: true})
			}
		}
	case "HTTPRoute":
		var hosts, paths []string
		for _, h := range sl(spec["hostnames"]) {
			hosts = append(hosts, s(h))
		}
		f["hosts"] = uniqSorted(hosts)
		for _, r := range sl(spec["rules"]) {
			matches := sl(m(r)["matches"])
			if len(matches) == 0 {
				paths = append(paths, "/") // a rule without matches is PathPrefix /
			}
			for _, mt := range matches {
				if v := s(path(m(mt), "path", "value")); v != "" {
					paths = append(paths, v)
				} else {
					paths = append(paths, "/")
				}
			}
		}
		if len(paths) > 0 {
			f["paths"] = uniqSorted(paths)
		}
		for _, p := range sl(spec["parentRefs"]) {
			pm := m(p)
			pns := s(pm["namespace"])
			if pns == "" {
				pns = ns
			}
			it.refs = append(it.refs, ref{"gateway.networking.k8s.io", "Gateway", pns, s(pm["name"]), RelAttaches})
		}
		for _, r := range sl(spec["rules"]) {
			for _, b := range sl(m(r)["backendRefs"]) {
				bm := m(b)
				if k := s(bm["kind"]); k == "" || k == "Service" {
					it.refs = append(it.refs, ref{"", "Service", ns, s(bm["name"]), RelRoutes})
					if name := s(bm["name"]); name != "" {
						it.backends = append(it.backends, backend{svc: name, port: anyString(bm["port"])})
					}
				}
			}
		}
	case "Gateway":
		f["class"] = s(spec["gatewayClassName"])
		var ls []string
		for _, l := range sl(spec["listeners"]) {
			lm := m(l)
			port, _ := num(lm["port"])
			ls = append(ls, s(lm["protocol"])+":"+itoa(port)+" "+s(lm["hostname"]))
			for _, c := range sl(path(lm, "tls", "certificateRefs")) {
				it.refs = append(it.refs, ref{"", "Secret", ns, s(m(c)["name"]), RelTLS})
			}
		}
		f["listeners"] = uniqSorted(ls)
	case "NetworkPolicy":
		var types []string
		for _, t := range sl(spec["policyTypes"]) {
			types = append(types, s(t))
		}
		f["policy_types"] = uniqSorted(types)
		if len(m(path(spec, "podSelector"))) == 0 {
			f["all_pods"] = true
		}
	case "ConfigMap":
		if noiseConfigMaps[it.res.Name] {
			return false
		}
		keys, digest := configMapDigest(obj)
		f["keys"] = keys
		f["content_sha256"] = digest
	case "ServiceAccount":
		identity := ""
		switch {
		case ann["eks.amazonaws.com/role-arn"] != "":
			identity = "aws-irsa"
		case ann["azure.workload.identity/client-id"] != "":
			identity = "azure-workload-identity"
		case ann["iam.gke.io/gcp-service-account"] != "":
			identity = "gcp-workload-identity"
		}
		pull := len(sl(obj["imagePullSecrets"]))
		if it.res.Name == "default" && identity == "" && pull == 0 {
			return false
		}
		if identity != "" {
			f["cloud_identity"] = identity
		}
		if pull > 0 {
			f["image_pull_secrets"] = pull
		}
	case "PersistentVolumeClaim":
		f["storage_class"] = s(spec["storageClassName"])
		f["size"] = s(path(spec, "resources", "requests", "storage"))
		f["access_modes"] = strList(spec["accessModes"])
		f["phase"] = s(status["phase"])
		if v := s(spec["volumeName"]); v != "" {
			f["volume"] = v
			it.refs = append(it.refs, ref{"", "PersistentVolume", "", v, RelBinds})
		}
		if sc := s(spec["storageClassName"]); sc != "" {
			it.refs = append(it.refs, ref{"storage.k8s.io", "StorageClass", "", sc, RelBinds})
		}
	case "PersistentVolume":
		f["storage_class"] = s(spec["storageClassName"])
		f["capacity"] = s(path(spec, "capacity", "storage"))
		f["access_modes"] = strList(spec["accessModes"])
		f["reclaim_policy"] = s(spec["persistentVolumeReclaimPolicy"])
		f["phase"] = s(status["phase"])
		f["driver"] = volumeDriver(spec)
		if c := m(spec["claimRef"]); c != nil {
			f["claim"] = s(c["namespace"]) + "/" + s(c["name"])
		}
	case "StorageClass":
		prov := s(obj["provisioner"])
		f["provisioner"] = prov
		f["reclaim_policy"] = s(obj["reclaimPolicy"])
		f["binding_mode"] = s(obj["volumeBindingMode"])
		if ann["storageclass.kubernetes.io/is-default-class"] == "true" {
			f["default"] = true
		}
		if b, _ := obj["allowVolumeExpansion"].(bool); b {
			f["expansion"] = true
		}
		if strings.HasPrefix(prov, "kubernetes.io/") {
			f["end_of_life"] = "in-tree provisioner; target: CSI driver"
		}
	case "VolumeSnapshot":
		f["class"] = s(spec["volumeSnapshotClassName"])
		if p := s(path(spec, "source", "persistentVolumeClaimName")); p != "" {
			f["source_pvc"] = p
			it.refs = append(it.refs, ref{"", "PersistentVolumeClaim", ns, p, RelMounts})
		}
	case "HorizontalPodAutoscaler":
		tr := m(spec["scaleTargetRef"])
		f["target"] = s(tr["kind"]) + "/" + s(tr["name"])
		if v, ok := num(spec["minReplicas"]); ok {
			f["min"] = v
		}
		if v, ok := num(spec["maxReplicas"]); ok {
			f["max"] = v
		}
		it.refs = append(it.refs, ref{groupOfAPIVersion(s(tr["apiVersion"])), s(tr["kind"]), ns, s(tr["name"]), RelScales})
	case "PodDisruptionBudget":
		if v := spec["minAvailable"]; v != nil {
			f["min_available"] = anyString(v)
		}
		if v := spec["maxUnavailable"]; v != nil {
			f["max_unavailable"] = anyString(v)
		}
		it.selector = strMap(path(spec, "selector", "matchLabels"))
	case "ResourceQuota":
		f["hard"] = mapKeys(m(spec["hard"]))
	case "LimitRange":
		f["limits"] = len(sl(spec["limits"]))
	case "Role":
		f["rules"] = len(sl(obj["rules"]))
	case "RoleBinding":
		rr := m(obj["roleRef"])
		f["role_ref"] = s(rr["kind"]) + "/" + s(rr["name"])
		if s(rr["kind"]) == "Role" {
			it.refs = append(it.refs, ref{"rbac.authorization.k8s.io", "Role", ns, s(rr["name"]), RelGrants})
		}
		subj := sl(obj["subjects"])
		f["subjects"] = len(subj)
		for _, sb := range subj {
			sm := m(sb)
			if s(sm["kind"]) == "ServiceAccount" {
				sns := s(sm["namespace"])
				if sns == "" {
					sns = ns
				}
				it.refs = append(it.refs, ref{"", "ServiceAccount", sns, s(sm["name"]), RelGrants})
			}
		}
	default:
		f["api_version"] = u.GetAPIVersion()
		nodeProvisioningFacts(it.res.Group+"/"+it.res.Kind, u, f)
	}
	return true
}

// Node labels that Karpenter, EKS Auto Mode and the cloud providers set.
const (
	labelInstanceType = "node.kubernetes.io/instance-type"
	labelZone         = "topology.kubernetes.io/zone"
	labelCapacityType = "karpenter.sh/capacity-type"
	labelNodePool     = "karpenter.sh/nodepool"
)

// nodeProvisioningFacts reads the Karpenter and EKS Auto Mode kinds (NodeClaim, NodePool,
// NodeClass, EC2NodeClass) from labels and spec. Status is never read, except the node name of a
// NodeClaim, which is volatile.
func nodeProvisioningFacts(groupKind string, u *unstructured.Unstructured, f map[string]any) {
	obj := u.Object
	spec := m(obj["spec"])
	switch groupKind {
	case "karpenter.sh/NodeClaim":
		reqs := sl(spec["requirements"])
		for fact, label := range map[string]string{"instance_type": labelInstanceType, "zone": labelZone,
			"capacity_type": labelCapacityType, "nodepool": labelNodePool} {
			if v := u.GetLabels()[label]; v != "" {
				f[fact] = v
			} else if v := singleRequirement(reqs, label); v != "" {
				f[fact] = v
			}
		}
		if ref := nodeClassRef(spec["nodeClassRef"]); ref != "" {
			f["node_class"] = ref
		}
		if n := s(path(obj, "status", "nodeName")); n != "" {
			f["node"] = n
		}
	case "karpenter.sh/NodePool":
		tpl := m(path(spec, "template", "spec"))
		if reqs := requirementsSummary(sl(tpl["requirements"])); len(reqs) > 0 {
			f["requirements"] = reqs
		}
		if ref := nodeClassRef(tpl["nodeClassRef"]); ref != "" {
			f["node_class"] = ref
		}
		if v := s(tpl["expireAfter"]); v != "" {
			f["expire_after"] = v
		}
		var limits []string
		for _, k := range mapKeys(m(spec["limits"])) {
			limits = append(limits, k+"="+anyString(m(spec["limits"])[k]))
		}
		if len(limits) > 0 {
			f["limits"] = limits
		}
		if w, ok := num(spec["weight"]); ok {
			f["weight"] = w
		}
		d := m(spec["disruption"])
		if v := s(d["consolidationPolicy"]); v != "" {
			f["consolidation_policy"] = v
		}
		if v := anyString(d["consolidateAfter"]); v != "" {
			f["consolidate_after"] = v
		}
		var budgets []string
		for _, b := range sl(d["budgets"]) {
			bm := m(b)
			txt := "nodes=" + anyString(bm["nodes"])
			if r := strList(bm["reasons"]); len(r) > 0 {
				txt += " reasons=" + strings.Join(r, ",")
			}
			if sc := s(bm["schedule"]); sc != "" {
				txt += " schedule=" + sc + " for " + s(bm["duration"])
			}
			budgets = append(budgets, txt)
		}
		if len(budgets) > 0 {
			f["disruption_budgets"] = budgets
		}
	case "eks.amazonaws.com/NodeClass":
		es := m(spec["ephemeralStorage"])
		disk := map[string]any{}
		if v := anyString(es["size"]); v != "" {
			disk["size"] = v
		}
		for _, k := range []string{"iops", "throughput"} {
			if n, ok := num(es[k]); ok {
				disk[k] = n
			}
		}
		if len(disk) > 0 {
			f["ephemeral_storage"] = disk
		}
	case "karpenter.k8s.aws/EC2NodeClass":
		var devs []map[string]any
		for _, b := range sl(spec["blockDeviceMappings"]) {
			bm := m(b)
			ebs := m(bm["ebs"])
			dev := map[string]any{"device": s(bm["deviceName"])}
			if v := s(ebs["volumeType"]); v != "" {
				dev["type"] = v
			}
			if v := anyString(ebs["volumeSize"]); v != "" {
				dev["size"] = v
			}
			for _, k := range []string{"iops", "throughput"} {
				if n, ok := num(ebs[k]); ok {
					dev[k] = n
				}
			}
			devs = append(devs, dev)
		}
		if len(devs) > 0 {
			f["block_devices"] = devs
		}
	}
}

// singleRequirement is the value of a requirement on key that allows exactly one value.
func singleRequirement(reqs []any, key string) string {
	for _, r := range reqs {
		rm := m(r)
		if s(rm["key"]) == key && s(rm["operator"]) == "In" {
			if v := sl(rm["values"]); len(v) == 1 {
				return s(v[0])
			}
		}
	}
	return ""
}

// requirementsSummary writes each requirement as "key Op v1,v2" (values sorted), sorted by key.
func requirementsSummary(reqs []any) []string {
	var out []string
	for _, r := range reqs {
		rm := m(r)
		txt := s(rm["key"]) + " " + s(rm["operator"])
		if v := strList(rm["values"]); len(v) > 0 {
			txt += " " + strings.Join(v, ",")
		}
		if n, ok := num(rm["minValues"]); ok {
			txt += " (min " + itoa(n) + ")"
		}
		out = append(out, txt)
	}
	sort.Strings(out)
	return out
}

// nodeClassRef names the node class of a NodeClaim or NodePool as group/Kind/name (v1 has group,
// v1beta1 has apiVersion).
func nodeClassRef(v any) string {
	r := m(v)
	if s(r["name"]) == "" {
		return ""
	}
	g := s(r["group"])
	if g == "" {
		g = groupOfAPIVersion(s(r["apiVersion"]))
	}
	return g + "/" + s(r["kind"]) + "/" + s(r["name"])
}

// configMapDigest returns sorted key names and a sha256 over sorted key=value pairs. Values are
// read only to hash them and never stored.
func configMapDigest(obj map[string]any) ([]string, string) {
	kv := map[string]string{}
	for _, field := range []string{"data", "binaryData"} {
		for k, v := range m(obj[field]) {
			kv[field+":"+k] = s(v)
		}
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(kv[k]))
		h.Write([]byte{0})
		names = append(names, k[strings.IndexByte(k, ':')+1:])
	}
	return uniqSorted(names), hex.EncodeToString(h.Sum(nil))[:16]
}

func volumeDriver(spec map[string]any) string {
	if d := s(path(spec, "csi", "driver")); d != "" {
		return d
	}
	for _, k := range []string{"awsElasticBlockStore", "gcePersistentDisk", "azureDisk", "azureFile", "nfs",
		"hostPath", "local", "cephfs", "rbd", "iscsi", "fc", "cinder", "vsphereVolume"} {
		if spec[k] != nil {
			return "in-tree:" + k
		}
	}
	return ""
}

func strList(v any) []string {
	var out []string
	for _, x := range sl(v) {
		out = append(out, s(x))
	}
	return uniqSorted(out)
}

func mapKeys(mm map[string]any) []string {
	out := make([]string, 0, len(mm))
	for k := range mm {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func anyString(v any) string {
	if n, ok := num(v); ok {
		return itoa(n)
	}
	return s(v)
}

func itoa(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func groupOfAPIVersion(av string) string {
	if i := strings.IndexByte(av, '/'); i >= 0 {
		return av[:i]
	}
	return ""
}
