// Package compare pairs the resources of a source and a target snapshot by identity
// (group/Kind/namespace/name) and classifies each one:
//
//	equal    in both, same fingerprint
//	drift    in both, different fingerprint; Diff names the facts that differ
//	pending  only in the source: not migrated yet
//	new      only in the target
//
// The result is deterministic: the same snapshots give the same document, byte for byte.
package compare

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/leosavio/kubestrata/internal/collect"
	"github.com/leosavio/kubestrata/internal/snapshot"
)

// SchemaVersion of the comparison document. Consumers must refuse a higher value.
const SchemaVersion = 1

// States of a resource.
const (
	Equal   = "equal"
	Drift   = "drift"
	Pending = "pending"
	New     = "new"
)

// Result is the comparison of two snapshots.
type Result struct {
	SchemaVersion int  `json:"schema_version"`
	Source        Side `json:"source"`
	Target        Side `json:"target"`
	// Totals cover the namespaces that are not system namespaces, like the default view.
	Totals     Summary   `json:"totals"`
	Namespaces []Summary `json:"namespaces"`
	Items      []Item    `json:"items"`
}

// Side identifies one of the two snapshots.
type Side struct {
	ClusterID   string    `json:"cluster_id"`
	Context     string    `json:"context"`
	Distro      string    `json:"distro"`
	Version     string    `json:"version"`
	CollectedAt time.Time `json:"collected_at"`
	Resources   int       `json:"resources"`
}

// Summary counts the states of one namespace ("_" for cluster-scoped resources).
type Summary struct {
	NS      string `json:"ns"`
	Team    string `json:"team,omitempty"`
	System  bool   `json:"system,omitempty"`
	Source  int    `json:"source"`
	Target  int    `json:"target"`
	Equal   int    `json:"equal"`
	Drift   int    `json:"drift"`
	Pending int    `json:"pending"`
	New     int    `json:"new"`
	// Coverage = (equal + drift) / source, rounded to 4 decimals; null when the source has
	// nothing in the namespace.
	Coverage *float64 `json:"coverage"`
}

// Item is the state of one identity.
type Item struct {
	ID    string `json:"id"`
	NS    string `json:"ns,omitempty"`
	Kind  string `json:"kind"`
	State string `json:"state"`
	Diff  []Diff `json:"diff,omitempty"`
}

// Diff is one fingerprint-relevant fact that differs. A side without the fact has no value.
type Diff struct {
	Fact   string `json:"fact"`
	Source any    `json:"source,omitempty"`
	Target any    `json:"target,omitempty"`
}

// Compare classifies every resource of src and tgt.
func Compare(src, tgt *snapshot.Snapshot) *Result {
	res := &Result{SchemaVersion: SchemaVersion, Source: side(src), Target: side(tgt),
		Namespaces: []Summary{}, Items: []Item{}}
	a, b := index(src), index(tgt)
	ids := make([]string, 0, len(a)+len(b))
	for id := range a {
		ids = append(ids, id)
	}
	for id := range b {
		if _, ok := a[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	// namespace team and system flag: the source wins, the target fills the gaps
	nsInfo := map[string]snapshot.Namespace{}
	for _, n := range tgt.Namespaces {
		nsInfo[n.Name] = n
	}
	for _, n := range src.Namespaces {
		if t, ok := nsInfo[n.Name]; ok {
			if n.Team == "" {
				n.Team = t.Team
			}
			n.System = n.System || t.System
		}
		nsInfo[n.Name] = n
	}
	per := map[string]*Summary{}
	sum := func(ns string) *Summary {
		if s, ok := per[ns]; ok {
			return s
		}
		s := &Summary{NS: ns}
		if n, ok := nsInfo[ns]; ok {
			s.Team, s.System = n.Team, n.System
		}
		per[ns] = s
		return s
	}

	for _, id := range ids {
		ra, inA := a[id]
		rb, inB := b[id]
		r := ra
		if !inA {
			r = rb
		}
		it := Item{ID: id, NS: r.NS, Kind: r.Kind}
		switch {
		case inA && inB:
			it.State = Equal
			if ra.FP == "" || ra.FP != rb.FP {
				// without fingerprints, the relevant facts decide
				if diff := diffFacts(ra.Facts, rb.Facts); len(diff) > 0 || ra.FP != rb.FP {
					it.State, it.Diff = Drift, diff
				}
			}
		case inA:
			it.State = Pending
		default:
			it.State = New
		}
		ns := r.NS
		if ns == "" {
			ns = snapshot.ClusterScoped
		}
		s := sum(ns)
		if s.Team == "" && r.Team != "" {
			s.Team = r.Team
		}
		count(s, it.State, inA, inB)
		res.Items = append(res.Items, it)
	}

	keys := make([]string, 0, len(per))
	for k := range per {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		x, y := keys[i], keys[j]
		if (x == snapshot.ClusterScoped) != (y == snapshot.ClusterScoped) {
			return y == snapshot.ClusterScoped
		}
		return x < y
	})
	tot := Summary{NS: "total"}
	for _, k := range keys {
		s := per[k]
		s.Coverage = coverage(s)
		res.Namespaces = append(res.Namespaces, *s)
		if !s.System {
			tot.Source += s.Source
			tot.Target += s.Target
			tot.Equal += s.Equal
			tot.Drift += s.Drift
			tot.Pending += s.Pending
			tot.New += s.New
		}
	}
	tot.Coverage = coverage(&tot)
	res.Totals = tot
	return res
}

// Find returns the item of an identity; false when neither side has it.
func (r *Result) Find(id string) (Item, bool) {
	i := sort.Search(len(r.Items), func(i int) bool { return r.Items[i].ID >= id })
	if i < len(r.Items) && r.Items[i].ID == id {
		return r.Items[i], true
	}
	return Item{}, false
}

func side(s *snapshot.Snapshot) Side {
	return Side{ClusterID: s.Cluster.ID, Context: s.Cluster.Context, Distro: s.Cluster.Distro,
		Version: s.Cluster.Version, CollectedAt: s.CollectedAt.UTC(), Resources: len(s.Resources)}
}

func index(s *snapshot.Snapshot) map[string]snapshot.Resource {
	m := make(map[string]snapshot.Resource, len(s.Resources))
	for _, r := range s.Resources {
		m[r.ID] = r
	}
	return m
}

func count(s *Summary, state string, inA, inB bool) {
	if inA {
		s.Source++
	}
	if inB {
		s.Target++
	}
	switch state {
	case Equal:
		s.Equal++
	case Drift:
		s.Drift++
	case Pending:
		s.Pending++
	case New:
		s.New++
	}
}

func coverage(s *Summary) *float64 {
	if s.Source == 0 {
		return nil
	}
	c := math.Round(float64(s.Equal+s.Drift)/float64(s.Source)*10000) / 10000
	return &c
}

// diffFacts lists the fingerprint-relevant facts whose values differ, sorted by name.
func diffFacts(a, b map[string]any) []Diff {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		if !collect.Volatile(k) {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	var out []Diff
	for _, k := range names {
		va, okA := a[k]
		vb, okB := b[k]
		if okA && okB && sameJSON(va, vb) {
			continue
		}
		out = append(out, Diff{Fact: k, Source: va, Target: vb})
	}
	return out
}

// sameJSON compares values the way they are stored: a snapshot read from the cache holds
// float64 and []any where a fresh one holds int64 and []string.
func sameJSON(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}
