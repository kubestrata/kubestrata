package collect

import (
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Managers: how an object got into the cluster. Drives the migration path.
const (
	MgrArgoCD   = "argocd"
	MgrFlux     = "flux"
	MgrHelm     = "helm"
	MgrKubectl  = "kubectl"
	MgrOperator = "operator"
	MgrOther    = "other"
	MgrUnknown  = "unknown"
)

// classifyManager prefers explicit labels and annotations, then the creating field manager.
func classifyManager(labels, ann map[string]string, mf []metav1.ManagedFieldsEntry, ownedByCR bool) (string, []string) {
	names := managerNames(mf)
	switch {
	case labels["argocd.argoproj.io/instance"] != "" || ann["argocd.argoproj.io/tracking-id"] != "":
		return MgrArgoCD, names
	case hasPrefixKey(labels, "kustomize.toolkit.fluxcd.io/") || hasPrefixKey(labels, "helm.toolkit.fluxcd.io/"):
		return MgrFlux, names
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
	case strings.HasPrefix(l, "kubectl"):
		return MgrKubectl
	}
	return MgrOther
}

// creatorManager is the manager of the oldest non-status entry: who created the object.
func creatorManager(mf []metav1.ManagedFieldsEntry) string {
	var best string
	var bestT time.Time
	for _, e := range mf {
		if e.Subresource != "" || e.Time == nil {
			continue
		}
		if best == "" || e.Time.Time.Before(bestT) {
			best, bestT = e.Manager, e.Time.Time
		}
	}
	return best
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
	for _, m := range maps {
		for _, k := range keys {
			if v := m[k]; v != "" {
				return v
			}
		}
	}
	return ""
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
