package compare

import (
	"sort"
	"strings"

	"github.com/kubestrata/kubestrata/internal/collect"
	"github.com/kubestrata/kubestrata/internal/snapshot"
)

// Kind translations: an OpenShift Route served in the target by an Ingress or a Gateway API
// HTTPRoute, an Ingress by an HTTPRoute, a DeploymentConfig by a Deployment. The identity key holds the kind, so these pairs
// never meet by id; Compare pairs what is left pending in the source with what is new in the
// target, in the same namespace.
const (
	gkRoute     = "route.openshift.io/Route"
	gkDC        = "apps.openshift.io/DeploymentConfig"
	gkIngress   = "networking.k8s.io/Ingress"
	gkHTTPRoute = "gateway.networking.k8s.io/HTTPRoute"
	gkDeploy    = "apps/Deployment"
)

// dcOnly are DeploymentConfig facts a Deployment cannot express.
var dcOnly = map[string]bool{"triggers": true, "strategy": true, "end_of_life": true}

// nameOf reads the name from an id (group/Kind/namespace/name; names hold no slash).
func nameOf(id string) string { return id[strings.LastIndex(id, "/")+1:] }

// groupKind reads group/Kind from an id (group/Kind/namespace/name).
func groupKind(id string) string {
	parts := strings.SplitN(id, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// translatesTo lists, per source group/Kind, the target group/Kinds that can serve it: a Route by
// an Ingress or an HTTPRoute, an Ingress by an HTTPRoute (Ingress to Gateway API), a
// DeploymentConfig by a Deployment.
var translatesTo = map[string]map[string]bool{
	gkRoute:   {gkIngress: true, gkHTTPRoute: true},
	gkIngress: {gkHTTPRoute: true},
	gkDC:      {gkDeploy: true},
}

// hostsOf reads the hosts a Route (host) or an Ingress or HTTPRoute (hosts) serves.
func hostsOf(r snapshot.Resource) []string {
	if groupKind(r.ID) == gkRoute {
		return strs([]any{str(r.Facts["host"])})
	}
	return strs(r.Facts["hosts"])
}

func overlaps(a, b []string) bool {
	for _, v := range a {
		if contains(b, v) {
			return true
		}
	}
	return false
}

// pairTranslations returns source id → target id. A Route pairs with an unpaired Ingress or
// HTTPRoute of its namespace, an Ingress with an unpaired HTTPRoute: one that serves one of its
// hosts, the same name first; without a host match, one of the same name. A DeploymentConfig pairs
// with the Deployment of the same namespace and name. Sources go in id order, candidates too, and
// each target pairs once.
func pairTranslations(a, b map[string]snapshot.Resource) map[string]string {
	var sources []string
	for id := range a {
		if _, ok := b[id]; ok {
			continue
		}
		if _, ok := translatesTo[groupKind(id)]; ok {
			sources = append(sources, id)
		}
	}
	sort.Strings(sources)
	byNS := map[string][]string{}
	for id, r := range b {
		if _, ok := a[id]; ok {
			continue
		}
		switch groupKind(id) {
		case gkIngress, gkHTTPRoute, gkDeploy:
			byNS[r.NS] = append(byNS[r.NS], id)
		}
	}
	for ns := range byNS {
		sort.Strings(byNS[ns])
	}
	used := map[string]bool{}
	out := map[string]string{}
	for _, id := range sources {
		r := a[id]
		accepts := translatesTo[groupKind(id)]
		hosts := hostsOf(r)
		best, score := "", 0
		for _, tid := range byNS[r.NS] {
			t := b[tid]
			gk := groupKind(tid)
			if used[tid] || !accepts[gk] {
				continue
			}
			sc := 0
			if nameOf(tid) == nameOf(id) {
				sc = 1
			}
			if gk != gkDeploy && overlaps(hosts, hostsOf(t)) {
				sc += 2
			}
			if gk == gkDeploy && sc == 0 {
				continue
			}
			if sc > score {
				best, score = tid, sc
			}
		}
		if best != "" {
			out[id] = best
			used[best] = true
		}
	}
	return out
}

// translatedDiff lists the facts both kinds express that differ: host, path, TLS on or off,
// backend Services and their ports for a Route and its Ingress or HTTPRoute; the pod template
// facts (images, replicas, requests, service account, ...) for a DeploymentConfig and its
// Deployment. A fact one side cannot tell (a snapshot of an older collector, an HTTPRoute
// without its Gateway) is not compared.
func translatedDiff(ra, rb snapshot.Resource, src, tgt *snapshot.Snapshot) []Diff {
	var va, vb map[string]any
	if groupKind(ra.ID) == gkDC {
		va, vb = podView(ra.Facts), podView(rb.Facts)
	} else {
		va, vb = routeView(ra, src), routeView(rb, tgt)
	}
	var keys []string
	for k := range va {
		if _, ok := vb[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []Diff
	for _, k := range keys {
		if !sameJSON(va[k], vb[k]) {
			out = append(out, Diff{Fact: k, Source: va[k], Target: vb[k]})
		}
	}
	return out
}

func podView(f map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range f {
		if !dcOnly[k] && !collect.Volatile(k) {
			out[k] = v
		}
	}
	return out
}

// routeView normalizes a Route, an Ingress or an HTTPRoute to hosts, paths, tls, backends
// (Service names) and ports (service:port, when the snapshot has them).
func routeView(r snapshot.Resource, s *snapshot.Snapshot) map[string]any {
	f := r.Facts
	out := map[string]any{}
	switch groupKind(r.ID) {
	case gkRoute:
		out["hosts"] = strs([]any{str(f["host"])})
		p := str(f["path"])
		if p == "" {
			p = "/"
		}
		out["paths"] = []string{p}
		out["tls"] = str(f["tls_termination"]) != ""
	default:
		out["hosts"] = strs(f["hosts"])
		if p, ok := f["paths"]; ok {
			out["paths"] = strs(p)
		}
		if t, ok := f["tls"].(bool); ok {
			out["tls"] = t
		} else if groupKind(r.ID) == gkHTTPRoute {
			if t, ok := gatewayTLS(r.ID, s); ok {
				out["tls"] = t
			}
		}
	}
	var svcs []string
	for _, e := range s.Edges {
		if e.From == r.ID && e.Rel == collect.RelRoutes && groupKind(e.To) == "core/Service" {
			svcs = append(svcs, nameOf(e.To))
		}
	}
	for _, m := range strs(f["missing_refs"]) {
		if name, ok := strings.CutPrefix(m, "Service/"); ok {
			svcs = append(svcs, name)
		}
	}
	if len(svcs) > 0 {
		out["backends"] = uniq(svcs)
	}
	var ports []string
	for _, b := range strs(f["backends"]) {
		if strings.Contains(b, ":") {
			ports = append(ports, b)
		}
	}
	if len(ports) > 0 {
		out["ports"] = uniq(ports)
	}
	return out
}

// gatewayTLS reports whether a Gateway the HTTPRoute attaches to has an HTTPS or TLS listener;
// false when none of them has one, unknown when the snapshot has none of its Gateways.
func gatewayTLS(id string, s *snapshot.Snapshot) (bool, bool) {
	gws := map[string]bool{}
	for _, e := range s.Edges {
		if e.From == id && e.Rel == collect.RelAttaches {
			gws[e.To] = true
		}
	}
	found, tls := false, false
	for _, r := range s.Resources {
		if !gws[r.ID] {
			continue
		}
		found = true
		for _, l := range strs(r.Facts["listeners"]) {
			if strings.HasPrefix(l, "HTTPS:") || strings.HasPrefix(l, "TLS:") {
				tls = true
			}
		}
	}
	return tls, found
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// strs reads a list of strings as stored: []string in a fresh snapshot, []any from the cache.
func strs(v any) []string {
	var out []string
	switch x := v.(type) {
	case []string:
		out = append(out, x...)
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	}
	return uniq(out)
}

func uniq(in []string) []string {
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

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
