package collect

import (
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubestrata/kubestrata/internal/snapshot"
)

// Managers: how an object got into the cluster. Drives the migration path.
const (
	MgrArgoCD    = "argocd"
	MgrFlux      = "flux"
	MgrHelm      = "helm"
	MgrTerraform = "terraform"
	MgrKubectl   = "kubectl"
	MgrOperator  = "operator"
	MgrPlatform  = "platform" // the cluster's own control plane and managed add-ons
	MgrKapp      = "kapp"     // Carvel kapp (also behind kapp-controller Apps)
	MgrConsole   = "console"  // edited in a web console (the field manager is a browser user agent)
	MgrOther     = "other"
	MgrUnknown   = "unknown"
)

// argoGroup is the API group of Argo CD.
const argoGroup = "argoproj.io"

// attributeArgoCD gives the manager argocd to every resource listed in the status.resources of a
// local Argo CD Application, whatever tracking method (label, annotation) the instance uses, and
// summarizes the result. An Application that deploys to another cluster lists objects of that
// cluster: it is counted as remote and its status is not used.
func attributeArgoCD(res []snapshot.Resource, lists []listed) *snapshot.ArgoCD {
	out := &snapshot.ArgoCD{}
	managed := map[string]bool{}
	for _, l := range lists {
		if l.api.Group != argoGroup || l.api.Kind != "Application" {
			continue
		}
		for _, u := range l.objs {
			out.Applications++
			if !localDestination(m(path(u.Object, "spec", "destination"))) {
				out.Remote++
				continue
			}
			for _, r := range sl(path(u.Object, "status", "resources")) {
				rm := m(r)
				if name := s(rm["name"]); name != "" {
					managed[snapshot.ID(s(rm["group"]), s(rm["kind"]), s(rm["namespace"]), name)] = true
				}
			}
		}
	}
	for i := range res {
		r := &res[i]
		if managed[r.ID] && r.Manager != MgrArgoCD {
			r.Manager = MgrArgoCD
			delete(r.Facts, "manager_raw")
			out.FromStatus++
		}
		if r.Manager == MgrArgoCD {
			out.Managed++
		}
	}
	return out
}

// localDestination reports whether an Application destination is the cluster Argo CD runs in.
func localDestination(dest map[string]any) bool {
	server, name := strings.TrimSuffix(s(dest["server"]), "/"), s(dest["name"])
	return server == "https://kubernetes.default.svc" || (server == "" && (name == "" || name == "in-cluster"))
}

// classifyManager prefers explicit labels and annotations, then the creating field manager.
func classifyManager(labels, ann map[string]string, mf []metav1.ManagedFieldsEntry, ownedByCR bool) (string, []string) {
	names := managerNames(mf)
	switch {
	case labels["argocd.argoproj.io/instance"] != "" || ann["argocd.argoproj.io/tracking-id"] != "":
		return MgrArgoCD, names
	case hasPrefixKey(labels, "kustomize.toolkit.fluxcd.io/") || hasPrefixKey(labels, "helm.toolkit.fluxcd.io/"):
		return MgrFlux, names
	case labels["kapp.k14s.io/app"] != "":
		return MgrKapp, names
	case strings.EqualFold(labels["app.kubernetes.io/managed-by"], "helm") || ann["meta.helm.sh/release-name"] != "":
		return MgrHelm, names
	case ownedByCR:
		return MgrOperator, names
	}
	if creator := creatorManager(mf); creator != "" {
		return mapManager(creator), names
	}
	return MgrUnknown, names
}

func mapManager(m string) string {
	l := strings.ToLower(m)
	switch {
	case strings.Contains(l, "argocd"):
		return MgrArgoCD
	case l == "helm" || strings.HasPrefix(l, "helm/"):
		return MgrHelm
	case l == "kustomize-controller" || l == "helm-controller":
		return MgrFlux
	// Terraform: the kubernetes and kubectl providers write as "HashiCorp" (user agent), server-side
	// apply defaults to "Terraform", the helm provider uses its binary name
	case l == "hashicorp" || l == "terraform" || strings.HasPrefix(l, "terraform-provider-"):
		return MgrTerraform
	case strings.HasPrefix(l, "kubectl"):
		return MgrKubectl
	case l == "kapp" || l == "kapp-controller":
		return MgrKapp
	// the API server names the field manager after the user agent up to the first slash: an edit
	// in the OpenShift or another web console arrives as "Mozilla"
	case strings.HasPrefix(m, "Mozilla"):
		return MgrConsole
	case platformManagers[m]:
		return MgrPlatform
	}
	return MgrOther
}

// systemManagers write fields of objects they did not create (token controller, PV binder,
// node lifecycle). They count as creator only when no other manager touched the object.
var systemManagers = map[string]bool{
	"kube-controller-manager": true, "kube-scheduler": true, "kubelet": true, "kube-apiserver": true,
	"cloud-controller-manager": true,
}

// platformManagers belong to the cluster itself: the control plane (systemManagers), the
// bootstrapper (kubeadm, which kind also uses), the EKS control plane and Auto Mode, the OpenShift
// cluster version operator. What they created is recreated by the target platform.
var platformManagers = func() map[string]bool {
	p := map[string]bool{"eks": true, "eks-auto-mode": true, "eks-internal": true, "kubeadm": true,
		"cluster-version-operator": true}
	for m := range systemManagers {
		p[m] = true
	}
	return p
}()

// creatorManager is the manager of the oldest non-status entry: who created the object. Ties at
// the same second go to a non-system manager, then to a non-platform one (EKS Auto Mode writes
// defaults into the NodePools Terraform creates, in the same second).
func creatorManager(mf []metav1.ManagedFieldsEntry) string {
	pick := func(skipSystem bool) string {
		var best string
		var bestT time.Time
		for _, e := range mf {
			if e.Subresource != "" || e.Time == nil || (skipSystem && systemManagers[e.Manager]) {
				continue
			}
			t := e.Time.Time
			if best == "" || t.Before(bestT) || (t.Equal(bestT) && platformManagers[best] && !platformManagers[e.Manager]) {
				best, bestT = e.Manager, t
			}
		}
		return best
	}
	if m := pick(true); m != "" {
		return m
	}
	return pick(false)
}

func managerNames(mf []metav1.ManagedFieldsEntry) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range mf {
		if e.Subresource != "" || e.Manager == "" || seen[e.Manager] {
			continue
		}
		seen[e.Manager] = true
		out = append(out, e.Manager)
	}
	sort.Strings(out)
	return out
}

// changedAt is the newest spec/metadata change. Status writes are ignored on purpose:
// controllers update status constantly and everything would look recent.
func changedAt(mf []metav1.ManagedFieldsEntry) time.Time {
	var t time.Time
	for _, e := range mf {
		if e.Subresource != "" || e.Time == nil {
			continue
		}
		if e.Time.Time.After(t) {
			t = e.Time.Time
		}
	}
	return t.UTC()
}

func appOf(labels, ann map[string]string) string {
	for _, k := range []string{"app.kubernetes.io/instance", "app.kubernetes.io/name", "app", "argocd.argoproj.io/instance"} {
		if v := labels[k]; v != "" {
			return v
		}
	}
	return ann["meta.helm.sh/release-name"]
}

func teamOf(keys []string, maps ...map[string]string) string {
	v, _ := teamFrom(keys, maps...)
	return v
}

// teamFrom returns the first team value found, in key order within each map, and its key.
func teamFrom(keys []string, maps ...map[string]string) (string, string) {
	for _, m := range maps {
		for _, k := range keys {
			if v := m[k]; v != "" {
				return v, k
			}
		}
	}
	return "", ""
}

func hasPrefixKey(m map[string]string, prefix string) bool {
	for k := range m {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// detectDistro reads API groups, server host, version and node data. Signals are kept so a
// simulated distro (e.g. OpenShift CRDs on kind) is visible as such.
func detectDistro(groups map[string]bool, host, version string, providerIDs, nodeLabels []string) (string, []string) {
	var sig []string
	has := func(list []string, sub string) bool {
		for _, s := range list {
			if strings.Contains(s, sub) {
				return true
			}
		}
		return false
	}
	distro := "kubernetes"
	switch {
	case groups["config.openshift.io"] || groups["route.openshift.io"] || groups["apps.openshift.io"]:
		distro = "openshift"
		for _, g := range []string{"config.openshift.io", "route.openshift.io", "apps.openshift.io"} {
			if groups[g] {
				sig = append(sig, "group "+g)
			}
		}
	case strings.HasSuffix(host, ".eks.amazonaws.com") || strings.Contains(version, "-eks-") || has(nodeLabels, "eks.amazonaws.com/"):
		distro = "eks"
	case strings.Contains(host, ".azmk8s.io") || has(nodeLabels, "kubernetes.azure.com/"):
		distro = "aks"
	case strings.Contains(version, "-gke.") || has(nodeLabels, "cloud.google.com/gke-"):
		distro = "gke"
	case strings.Contains(version, "+orb"):
		distro = "orbstack"
	case strings.Contains(version, "+k3s"):
		distro = "k3s"
	case has(providerIDs, "kind://"):
		distro = "kind"
	}
	if has(providerIDs, "kind://") {
		sig = append(sig, "providerID kind://")
	}
	for _, mark := range []string{"+k3s", "+orb"} {
		if strings.Contains(version, mark) {
			sig = append(sig, "version "+mark)
		}
	}
	return distro, sig
}
