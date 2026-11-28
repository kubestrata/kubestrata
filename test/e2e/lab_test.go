//go:build lab

// Package e2e collects the two clusters of a running kuberito lab and compares the snapshots
// with the lab's ground truth. Run with `make lab-test` after `kuberito up`
// (https://github.com/kuberito/kuberito); tests skip when no lab is up.
package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kubestrata/kubestrata/internal/collect"
	"github.com/kubestrata/kubestrata/internal/compare"
	"github.com/kubestrata/kubestrata/internal/kube"
	"github.com/kubestrata/kubestrata/internal/render"
	"github.com/kubestrata/kubestrata/internal/snapshot"
)

// Values the lab seeds on purpose; none may reach a snapshot.
var forbidden = []string{
	"kuberito-demo-not-a-secret",
	base64.StdEncoding.EncodeToString([]byte("kuberito-demo-not-a-secret")),
	"last-applied-configuration",
	"000000000000",
}

type truth struct {
	SchemaVersion int    `json:"schema_version"`
	Context       string `json:"context"`
	Flavor        string `json:"flavor"`
	Objects       []struct {
		ID      string    `json:"id"`
		Manager string    `json:"manager"`
		Created time.Time `json:"created"`
		Changed time.Time `json:"changed"`
		Wave    string    `json:"wave"` // target only: "1", "2", "3" (migrated) or "new"
	} `json:"objects"`
}

func labHome() string {
	if h := os.Getenv("KUBERITO_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".kuberito")
}

func load(t *testing.T, side string) (truth, *snapshot.Snapshot) {
	t.Helper()
	home := labHome()
	b, err := os.ReadFile(filepath.Join(home, "truth", side+".json"))
	if err != nil {
		t.Skipf("no kuberito lab (%v): run `kuberito up` first", err)
	}
	var tr truth
	if err := json.Unmarshal(b, &tr); err != nil {
		t.Fatal(err)
	}
	if tr.SchemaVersion != 1 {
		t.Fatalf("truth schema_version %d not supported", tr.SchemaVersion)
	}
	src, err := kube.Open(filepath.Join(home, "kubeconfig"), tr.Context, "kubestrata-e2e")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := collect.Collect(context.Background(), src, collect.Options{Context: tr.Context, ToolVersion: "e2e", CRDs: true})
	if err != nil {
		t.Fatalf("collect %s: %v", tr.Context, err)
	}
	return tr, snap
}

// Kinds kubestrata folds into their owner by design.
var folded = map[string]bool{"ReplicationController": true, "ReplicaSet": true, "Job": true}

// Lab managers that are not delivery tools.
var noManager = map[string]bool{"platform": true, "openshift": true}

func TestLabMatchesTruth(t *testing.T) {
	for _, side := range []string{"source", "target"} {
		t.Run(side, func(t *testing.T) {
			tr, snap := load(t, side)
			if len(snap.Errors) > 0 {
				t.Errorf("collection errors: %+v", snap.Errors)
			}
			res := map[string]snapshot.Resource{}
			for _, r := range snap.Resources {
				res[r.ID] = r
			}
			ns := map[string]snapshot.Namespace{}
			for _, n := range snap.Namespaces {
				ns[n.Name] = n
			}
			checked := 0
			for _, o := range tr.Objects {
				kind := strings.SplitN(o.ID, "/", 3)[1]
				if folded[kind] {
					continue
				}
				if kind == "Namespace" {
					n, ok := ns[o.ID[strings.LastIndex(o.ID, "/")+1:]]
					if !ok || !n.Created.Equal(o.Created) {
						t.Errorf("%s: namespace missing or created %v, want %v", o.ID, n.Created, o.Created)
					}
					checked++
					continue
				}
				r, ok := res[o.ID]
				if !ok {
					t.Errorf("%s: not in snapshot", o.ID)
					continue
				}
				checked++
				if !r.Created.Equal(o.Created) {
					t.Errorf("%s: created %v, want %v", o.ID, r.Created, o.Created)
				}
				if !o.Changed.IsZero() && !r.Changed.Equal(o.Changed) {
					t.Errorf("%s: changed %v, want %v", o.ID, r.Changed, o.Changed)
				}
				if !noManager[o.Manager] && r.Manager != o.Manager {
					t.Errorf("%s: manager %s, want %s", o.ID, r.Manager, o.Manager)
				}
			}
			t.Logf("%s: %d truth objects checked, distro %s %v", side, checked, snap.Cluster.Distro, snap.Cluster.DistroSignals)
			if want := map[string]string{"openshift": "openshift", "eks": "eks"}[tr.Flavor]; want != "" && snap.Cluster.Distro != want {
				t.Errorf("distro %s, want %s", snap.Cluster.Distro, want)
			}
		})
	}
}

// TestLabCompare checks the comparison of the two lab clusters against the scenario. The lab
// may hold extra namespaces created by other runs (kuberito-tutorial, kuberito-age-test): the
// assertions only look at truth objects and at named namespaces, never at totals.
func TestLabCompare(t *testing.T) {
	_, src := load(t, "source")
	tt, tgt := load(t, "target")
	res := compare.Compare(src, tgt)
	state := func(id string) compare.Item {
		it, _ := res.Find(id)
		return it
	}
	diff := func(it compare.Item, fact string) (compare.Diff, bool) {
		for _, d := range it.Diff {
			if d.Fact == fact {
				return d, true
			}
		}
		return compare.Diff{}, false
	}

	api := state("apps/Deployment/payments/api")
	if d, ok := diff(api, "images"); api.State != compare.Drift || !ok ||
		!strings.Contains(fmt.Sprint(d.Source), ":2.56") || !strings.Contains(fmt.Sprint(d.Target), ":2.57") {
		t.Errorf("payments/api: %+v, want drift on images 2.56 → 2.57", api)
	}
	search := state("apps/Deployment/catalog/search")
	if _, ok := diff(search, "replicas"); search.State != compare.Drift || !ok {
		t.Errorf("catalog/search: %+v, want drift on replicas", search)
	}
	// kind translation: the target serves the OpenShift Route auth/sso with an HTTPRoute of the
	// same name. The pair drifts only on its hosts (the target's domain): TLS (edge termination,
	// the Gateway's HTTPS listener), path and backend Service stay the same.
	sso := state("route.openshift.io/Route/auth/sso")
	if sso.Translation != "Route→HTTPRoute" || sso.TargetID != "gateway.networking.k8s.io/HTTPRoute/auth/sso" ||
		sso.State != compare.Drift {
		t.Errorf("auth/sso: %+v, want the Route paired with the HTTPRoute, drift", sso)
	} else if d, ok := diff(sso, "hosts"); !ok || len(sso.Diff) != 1 {
		t.Errorf("auth/sso diff %+v, want hosts only", sso.Diff)
	} else {
		t.Logf("auth/sso: Route→HTTPRoute, hosts %v → %v", d.Source, d.Target)
	}
	translations := map[string]int{}
	for _, it := range res.Items {
		if it.Translation != "" {
			translations[it.Translation]++
		}
	}
	t.Logf("kind translations: %v (the lab migrates no DeploymentConfig)", translations)
	for _, id := range []string{"apps/Deployment/payments/fraud", "apps/Deployment/catalog/recs"} {
		if st := state(id).State; st != compare.New {
			t.Errorf("%s: %q, want new", id, st)
		}
	}
	pendingNS := map[string]int{"legacy-portal": 0, "data": 0}
	for _, it := range res.Items {
		if _, ok := pendingNS[it.NS]; !ok {
			continue
		}
		pendingNS[it.NS]++
		if it.State != compare.Pending {
			t.Errorf("%s: %q, want pending (namespace not migrated)", it.ID, it.State)
		}
	}
	for ns, n := range pendingNS {
		if n == 0 {
			t.Errorf("namespace %s: no resources compared", ns)
		}
	}

	// Waves 1-3 were migrated, so the source has them. A kind translation is the exception: the
	// target exposes the app with a Gateway API HTTPRoute where the source had an OpenShift Route
	// or an Ingress. A Route pairs with its HTTPRoute (equal or drift, found by the target id); an
	// Ingress does not, so that pair shows as pending + new.
	waves := map[string]int{}
	for _, o := range tt.Objects {
		kind := strings.SplitN(o.ID, "/", 3)[1]
		if o.Wave == "" || folded[kind] || kind == "Namespace" {
			continue
		}
		waves[o.Wave]++
		st := state(o.ID).State
		switch {
		case o.Wave == "new":
			if st != compare.New {
				t.Errorf("%s (wave new): %q, want new", o.ID, st)
			}
		case st == compare.Equal || st == compare.Drift:
		case kind == "HTTPRoute" && st == compare.New:
			p := strings.Split(o.ID, "/")
			ns, name := p[2], p[3]
			route, ing := state("route.openshift.io/Route/"+ns+"/"+name), state("networking.k8s.io/Ingress/"+ns+"/"+name)
			if route.State != compare.Pending && ing.State != compare.Pending {
				t.Errorf("%s (wave %s): new, and the source has no Route or Ingress %s/%s", o.ID, o.Wave, ns, name)
			} else {
				t.Logf("%s (wave %s): kind translated from %s", o.ID, o.Wave, map[bool]string{true: "Route", false: "Ingress"}[route.State == compare.Pending])
			}
		default:
			t.Errorf("%s (wave %s): %q, want equal or drift (migrated)", o.ID, o.Wave, st)
		}
	}
	if waves["1"] == 0 || waves["2"] == 0 || waves["3"] == 0 || waves["new"] == 0 {
		t.Errorf("truth waves: %v", waves)
	}
	tot := res.Totals
	t.Logf("totals: source %d, target %d, equal %d, drift %d, pending %d, new %d, coverage %.4f; truth waves checked %v",
		tot.Source, tot.Target, tot.Equal, tot.Drift, tot.Pending, tot.New, *tot.Coverage, waves)

	// the site of the comparison loads the same document
	dir := t.TempDir()
	if _, err := render.Write(dir, []*snapshot.Snapshot{src, tgt}, render.Options{Mode: "compare", Version: "e2e", Compare: res}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "json", "compare.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc compare.Result
	if err := json.Unmarshal(b, &doc); err != nil || doc.SchemaVersion != 1 || len(doc.Items) != len(res.Items) {
		t.Fatalf("json/compare.json: schema %d, %d items, err %v", doc.SchemaVersion, len(doc.Items), err)
	}
}

func TestLabNoSecretLeak(t *testing.T) {
	for _, side := range []string{"source", "target"} {
		_, snap := load(t, side)
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(snap); err != nil {
			t.Fatal(err)
		}
		for _, f := range forbidden {
			if i := bytes.Index(buf.Bytes(), []byte(f)); i >= 0 {
				t.Errorf("%s: snapshot contains %q near %q", side, f, buf.Bytes()[max(0, i-60):min(buf.Len(), i+20)])
			}
		}
	}
}
