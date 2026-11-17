package collect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/leosavio/kubestrata/internal/snapshot"
)

// Options of one collection.
type Options struct {
	Context     string
	ToolVersion string
	SystemNS    string
	TeamKeys    []string
	CRDs        bool
	Concurrency int
	Now         func() time.Time
}

type listed struct {
	spec kindSpec
	api  APIResource
	objs []unstructured.Unstructured
}

// builtinGroups are the API groups of the builtin table; an owner outside them is a custom
// resource (operator).
var builtinGroups = func() map[string]bool {
	g := map[string]bool{}
	for _, k := range builtin {
		g[k.Group] = true
	}
	return g
}()

// Collect lists the cluster through src and returns a normalized snapshot. Problems with single
// kinds go to Errors; an error is returned only when the cluster cannot be inventoried at all.
func Collect(ctx context.Context, src Source, o Options) (*snapshot.Snapshot, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 8
	}
	if o.SystemNS == "" {
		o.SystemNS = DefaultSystemNS
	}
	if len(o.TeamKeys) == 0 {
		o.TeamKeys = DefaultTeamKeys
	}
	sysRE, err := regexp.Compile(o.SystemNS)
	if err != nil {
		return nil, fmt.Errorf("system namespace pattern: %w", err)
	}
	version, err := src.ServerVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("server version: %w", err)
	}
	snap := &snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		Tool:          snapshot.Tool{Name: "kubestrata", Version: o.ToolVersion},
		CollectedAt:   o.Now().UTC().Truncate(time.Second),
		Scope:         snapshot.Scope{SystemNS: o.SystemNS, TeamKeys: o.TeamKeys},
	}
	var mu sync.Mutex
	addErr := func(kind, ns string, err error) {
		reason := string(apierrors.ReasonForError(err))
		if reason == "" {
			reason = "Error"
		}
		msg := err.Error()
		if len(msg) > 300 {
			msg = msg[:300]
		}
		mu.Lock()
		snap.Errors = append(snap.Errors, snapshot.Error{Kind: kind, NS: ns, Reason: reason, Message: msg})
		mu.Unlock()
	}

	uid, err := src.KubeSystemUID(ctx)
	if err != nil {
		addErr("core/Namespace", "kube-system", err)
	}
	server := src.Server()
	sum := sha256.Sum256([]byte(server + "|" + uid))
	snap.Cluster = snapshot.Cluster{ID: hex.EncodeToString(sum[:])[:12], Context: o.Context,
		ServerHost: snapshot.HostOf(server), Version: version}

	apis, derrs := src.Discover(ctx)
	for _, e := range derrs {
		addErr("discovery", "", e)
	}
	if len(apis) == 0 {
		return nil, fmt.Errorf("discovery returned no resources")
	}
	groups := map[string]bool{}
	var todo []listed
	for _, a := range apis {
		groups[a.Group] = true
		spec, ok := specFor(a.Group, a.Resource)
		if !ok {
			if !a.CRD || !o.CRDs {
				continue
			}
			spec = kindSpec{Group: a.Group, Resource: a.Resource, Kind: a.Kind, Cat: CatCRD, Mode: modeShow}
		}
		todo = append(todo, listed{spec: spec, api: a})
	}
	sort.Slice(todo, func(i, j int) bool {
		return todo[i].api.Group+"/"+todo[i].api.Resource < todo[j].api.Group+"/"+todo[j].api.Resource
	})

	var secrets []SecretMeta
	sem := make(chan struct{}, o.Concurrency)
	var wg sync.WaitGroup
	for i := range todo {
		wg.Add(1)
		go func(l *listed) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			gk := snapshot.GroupKind(l.api.Group, l.api.Kind)
			if l.spec.Mode == modeSecrets {
				sm, err := src.ListSecretsMeta(ctx)
				if err != nil {
					addErr(gk, "", err)
					return
				}
				mu.Lock()
				secrets = sm
				mu.Unlock()
				return
			}
			objs, err := src.List(ctx, l.api)
			if err != nil {
				addErr(gk, "", err)
				return
			}
			l.objs = objs
		}(&todo[i])
	}
	wg.Wait()

	b := &builder{o: o, sysRE: sysRE, byUID: map[string]*item{}, nsTeam: map[string]string{}}
	for _, l := range todo {
		if l.spec.Mode == modeNamespaces {
			b.namespaces(snap, l.objs)
		}
	}
	var providerIDs, nodeLabels []string
	for _, l := range todo {
		switch l.spec.Mode {
		case modeNodes:
			providerIDs, nodeLabels = b.nodes(snap, l.objs)
		case modeShow, modeSupport:
			for i := range l.objs {
				b.add(l.spec, l.api, &l.objs[i])
			}
		}
		if l.spec.Mode != modeNamespaces && l.spec.Mode != modeNodes && (l.objs != nil || l.spec.Mode == modeSecrets) {
			snap.Scope.Kinds = append(snap.Scope.Kinds, snapshot.GroupKind(l.api.Group, l.api.Kind))
		}
	}
	for i := range secrets {
		b.addSecret(&secrets[i])
	}
	snap.Cluster.Distro, snap.Cluster.DistroSignals = detectDistro(groups, snap.Cluster.ServerHost, version, providerIDs, nodeLabels)
	snap.Resources, snap.Edges = b.finish()
	snap.Normalize()
	return snap, nil
}

type builder struct {
	o        Options
	sysRE    *regexp.Regexp
	items    []*item
	byUID    map[string]*item
	nsTeam   map[string]string
	nodeArch map[string]string
}

func (b *builder) namespaces(snap *snapshot.Snapshot, objs []unstructured.Unstructured) {
	keys := append(append([]string{}, b.o.TeamKeys...), "openshift.io/requester")
	for _, u := range objs {
		team := teamOf(keys, u.GetLabels(), u.GetAnnotations())
		b.nsTeam[u.GetName()] = team
		snap.Namespaces = append(snap.Namespaces, snapshot.Namespace{
			Name: u.GetName(), Team: team, System: b.sysRE.MatchString(u.GetName()),
			Created: u.GetCreationTimestamp().UTC(),
		})
	}
}

func (b *builder) nodes(snap *snapshot.Snapshot, objs []unstructured.Unstructured) ([]string, []string) {
	b.nodeArch = map[string]string{}
	ns := snapshot.NodeSummary{Count: len(objs), Arch: map[string]int{}}
	zones := map[string]bool{}
	kubelets := map[string]bool{}
	labelKeys := map[string]bool{}
	var providers []string
	for _, u := range objs {
		arch := s(path(u.Object, "status", "nodeInfo", "architecture"))
		b.nodeArch[u.GetName()] = arch
		if arch != "" {
			ns.Arch[arch]++
		}
		if z := u.GetLabels()["topology.kubernetes.io/zone"]; z != "" {
			zones[z] = true
		}
		if v := s(path(u.Object, "status", "nodeInfo", "kubeletVersion")); v != "" {
			kubelets[v] = true
		}
		for k := range u.GetLabels() {
			labelKeys[k] = true
		}
		providers = append(providers, s(path(u.Object, "spec", "providerID")))
	}
	ns.Zones = setList(zones)
	ns.KubeletVersion = setList(kubelets)
	snap.Cluster.Nodes = ns
	return providers, setList(labelKeys)
}

func setList(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func controllerRef(refs []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			return &refs[i]
		}
	}
	if len(refs) > 0 {
		return &refs[0]
	}
	return nil
}

func (b *builder) base(spec kindSpec, api APIResource, ns, name string, created metav1.Time,
	labels, ann map[string]string, mf []metav1.ManagedFieldsEntry, owners []metav1.OwnerReference) *item {
	ownedByCR := false
	ownerUID := ""
	if c := controllerRef(owners); c != nil {
		ownerUID = string(c.UID)
		ownedByCR = !builtinGroups[groupOfAPIVersion(c.APIVersion)]
	}
	mgr, names := classifyManager(labels, ann, mf, ownedByCR)
	team := teamOf(b.o.TeamKeys, labels)
	if team == "" {
		team = b.nsTeam[ns]
	}
	facts := map[string]any{}
	if len(names) > 0 {
		facts["field_managers"] = names
	}
	return &item{
		ownerUID: ownerUID,
		res: snapshot.Resource{
			ID:      snapshot.ID(api.Group, api.Kind, ns, name),
			Group:   api.Group,
			Version: api.Version,
			Kind:    api.Kind,
			Cat:     spec.Cat,
			NS:      ns,
			Name:    name,
			Created: created.UTC(),
			Changed: changedAt(mf),
			Manager: mgr,
			App:     appOf(labels, ann),
			Team:    team,
			Labels:  labels,
			Facts:   facts,
		},
	}
}

func (b *builder) add(spec kindSpec, api APIResource, u *unstructured.Unstructured) {
	it := b.base(spec, api, u.GetNamespace(), u.GetName(), u.GetCreationTimestamp(), u.GetLabels(),
		u.GetAnnotations(), u.GetManagedFields(), u.GetOwnerReferences())
	it.uid = string(u.GetUID())
	if !kindFacts(it, u) {
		return
	}
	if spec.Mode == modeSupport {
		it.res.Facts["_support"] = true
		b.supportStatus(it, u)
	}
	b.items = append(b.items, it)
	if it.uid != "" {
		b.byUID[it.uid] = it
	}
}

// supportStatus keeps the status data used for aggregation into the owner.
func (b *builder) supportStatus(it *item, u *unstructured.Unstructured) {
	if it.res.Kind != "Pod" {
		return
	}
	ready := false
	for _, c := range sl(path(u.Object, "status", "conditions")) {
		cm := m(c)
		if s(cm["type"]) == "Ready" && s(cm["status"]) == "True" {
			ready = true
		}
	}
	var restarts int64
	for _, c := range sl(path(u.Object, "status", "containerStatuses")) {
		if n, ok := num(m(c)["restartCount"]); ok {
			restarts += n
		}
	}
	it.res.Facts["_ready"] = ready
	it.res.Facts["_restarts"] = restarts
}

func (b *builder) addSecret(sm *SecretMeta) {
	spec := kindSpec{Kind: "Secret", Cat: CatConfig, Mode: modeShow}
	api := APIResource{Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true}
	it := b.base(spec, api, sm.NS, sm.Name, metav1.NewTime(sm.Created), sm.Labels, nil, sm.ManagedFields, sm.OwnerRefs)
	it.uid = sm.UID
	it.res.Facts["type"] = sm.Type
	it.res.Facts["keys"] = sm.Keys
	switch {
	case sm.Type == "helm.sh/release.v1" || sm.Labels["owner"] == "helm":
		it.res.Manager = MgrHelm
		it.res.Facts["helm_release"] = sm.Labels["name"]
		it.res.Facts["helm_revision"] = sm.Labels["version"]
		it.res.Facts["helm_status"] = sm.Labels["status"]
	case sm.Type == "kubernetes.io/service-account-token":
		it.res.Facts["legacy_sa_token"] = true
	}
	b.items = append(b.items, it)
	if it.uid != "" {
		b.byUID[it.uid] = it
	}
}

func isSupport(it *item) bool {
	v, _ := it.res.Facts["_support"].(bool)
	return v
}

// finish resolves owners, aggregates support objects into the nearest shown ancestor, builds
// edges and fingerprints.
func (b *builder) finish() ([]snapshot.Resource, []snapshot.Edge) {
	shown := map[*item]bool{}
	for _, it := range b.items {
		owner := b.byUID[it.ownerUID]
		if !isSupport(it) || owner == nil {
			shown[it] = true
			if isSupport(it) && it.ownerUID != "" {
				it.res.Facts["owner_missing"] = true
			} else if isSupport(it) {
				it.res.Facts["orphan"] = true
			}
		}
	}
	var edges []snapshot.Edge
	for _, it := range b.items {
		if shown[it] {
			if owner := b.byUID[it.ownerUID]; owner != nil {
				it.res.Owner = owner.res.ID
				edges = append(edges, snapshot.Edge{From: owner.res.ID, To: it.res.ID, Rel: RelOwns})
			}
			continue
		}
		target := b.byUID[it.ownerUID]
		for target != nil && !shown[target] {
			target = b.byUID[target.ownerUID]
		}
		if target == nil {
			continue
		}
		aggregate(target, it, b.nodeArch)
	}

	byID := map[string]*item{}
	var out []*item
	for _, it := range b.items {
		if shown[it] {
			byID[it.res.ID] = it
			out = append(out, it)
		}
	}
	for _, it := range out {
		var missing []string
		for _, r := range it.refs {
			if r.name == "" {
				continue
			}
			id := snapshot.ID(r.group, r.kind, r.ns, r.name)
			if _, ok := byID[id]; ok && id != it.res.ID {
				edges = append(edges, snapshot.Edge{From: it.res.ID, To: id, Rel: r.rel})
			} else if !(r.kind == "ServiceAccount" && r.name == "default") {
				missing = append(missing, r.kind+"/"+r.name)
			}
		}
		if len(missing) > 0 {
			it.res.Facts["missing_refs"] = uniqSorted(missing)
		}
		if len(it.selector) > 0 && (it.res.Kind == "Service" || it.res.Kind == "PodDisruptionBudget") {
			matched := false
			for _, w := range out {
				if w.res.NS == it.res.NS && w.res.Cat == CatWorkload && w.tmplLabels != nil && subset(it.selector, w.tmplLabels) {
					edges = append(edges, snapshot.Edge{From: it.res.ID, To: w.res.ID, Rel: RelSelects})
					matched = true
				}
			}
			if !matched {
				it.res.Facts["selects_none"] = true
			}
		}
	}

	res := make([]snapshot.Resource, 0, len(out))
	for _, it := range out {
		for k := range it.res.Facts {
			if strings.HasPrefix(k, "_") {
				delete(it.res.Facts, k)
			}
		}
		it.res.FP = fingerprint(it.res.Kind, it.res.Facts)
		if len(it.res.Facts) == 0 {
			it.res.Facts = nil
		}
		res = append(res, it.res)
	}
	edges = dedupEdges(edges)
	return res, edges
}

func subset(sel, labels map[string]string) bool {
	for k, v := range sel {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// aggregate folds a support object (pod, replica set, job) into the shown workload.
func aggregate(target, it *item, nodeArch map[string]string) {
	f := target.res.Facts
	switch it.res.Kind {
	case "Pod":
		f["pods"] = asInt(f["pods"]) + 1
		if r, _ := it.res.Facts["_ready"].(bool); r {
			f["ready"] = asInt(f["ready"]) + 1
		}
		f["restarts"] = asInt(f["restarts"]) + asInt(it.res.Facts["_restarts"])
		setNewest(f, "pod_newest", it.res.Created)
		setOldest(f, "pod_oldest", it.res.Created)
		if arch := nodeArch[s(it.res.Facts["node"])]; arch != "" {
			f["archs"] = uniqSorted(append(toStrings(f["archs"]), arch))
		}
	case "ReplicaSet", "ReplicationController":
		if asInt(it.res.Facts["replicas"]) > 0 {
			setNewest(f, "last_rollout", it.res.Created)
		}
	case "Job":
		f["jobs"] = asInt(f["jobs"]) + 1
		setNewest(f, "last_run", it.res.Created)
	}
}

func asInt(v any) int64 {
	n, _ := num(v)
	return n
}

func toStrings(v any) []string {
	x, _ := v.([]string)
	return x
}

func setNewest(f map[string]any, key string, t time.Time) {
	if cur, ok := f[key].(time.Time); !ok || t.After(cur) {
		f[key] = t
	}
}

func setOldest(f map[string]any, key string, t time.Time) {
	if cur, ok := f[key].(time.Time); !ok || t.Before(cur) {
		f[key] = t
	}
}

func dedupEdges(in []snapshot.Edge) []snapshot.Edge {
	seen := map[snapshot.Edge]bool{}
	out := make([]snapshot.Edge, 0, len(in))
	for _, e := range in {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

// volatile facts change without a migration-relevant difference and stay out of fingerprints.
var volatile = map[string]bool{
	"pods": true, "ready": true, "restarts": true, "pod_newest": true, "pod_oldest": true,
	"last_rollout": true, "last_run": true, "jobs": true, "archs": true, "phase": true, "node": true,
	"volume": true, "claim": true, "field_managers": true, "missing_refs": true, "selects_none": true,
	"helm_revision": true, "helm_status": true, "orphan": true, "owner_missing": true,
}

func fingerprint(kind string, facts map[string]any) string {
	keep := map[string]any{"kind": kind}
	for k, v := range facts {
		if !volatile[k] {
			keep[k] = v
		}
	}
	b, _ := json.Marshal(keep)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}
