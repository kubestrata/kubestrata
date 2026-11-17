//go:build lab

// Package e2e collects the two clusters of a running kuberito lab and compares the snapshots
// with the lab's ground truth. Run with `make lab-test` after `kuberito up`
// (https://github.com/leosavio/kuberito); tests skip when no lab is up.
package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leosavio/kubestrata/internal/collect"
	"github.com/leosavio/kubestrata/internal/kube"
	"github.com/leosavio/kubestrata/internal/snapshot"
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
