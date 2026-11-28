// Package snapshot defines the inventory document kubestrata writes to its cache and embeds in
// the site. It is a contract: fields only grow, and SchemaVersion moves when a meaning changes.
package snapshot

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// SchemaVersion is the highest snapshot version this build reads and the one it writes.
const SchemaVersion = 1

// Snapshot is one read-only inventory of one cluster at one point in time.
type Snapshot struct {
	SchemaVersion int         `json:"schema_version"`
	Tool          Tool        `json:"tool"`
	CollectedAt   time.Time   `json:"collected_at"`
	Cluster       Cluster     `json:"cluster"`
	Scope         Scope       `json:"scope"`
	Namespaces    []Namespace `json:"namespaces"`
	Resources     []Resource  `json:"resources"`
	Edges         []Edge      `json:"edges"`
	Errors        []Error     `json:"errors"`
}

// Tool identifies the binary that produced the snapshot.
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Cluster describes the cluster. ID is stable across context renames.
type Cluster struct {
	ID            string      `json:"id"`
	Context       string      `json:"context"`
	ServerHost    string      `json:"server_host"`
	Distro        string      `json:"distro"`
	DistroSignals []string    `json:"distro_signals,omitempty"`
	Version       string      `json:"version"`
	Nodes         NodeSummary `json:"nodes"`
	// ArgoCD is set when the cluster serves the Argo CD Application kind (argoproj.io).
	ArgoCD *ArgoCD `json:"argocd,omitempty"`
}

// ArgoCD summarizes what Argo CD manages in the snapshot. Managed 0 with the kind served reads
// "Argo CD installed, no managed objects found": the Applications deploy elsewhere, cannot be
// read, or track nothing that was collected.
type ArgoCD struct {
	// Applications collected; Remote of them deploy to another cluster, so their status is not
	// used here.
	Applications int `json:"applications"`
	Remote       int `json:"remote,omitempty"`
	// Managed is the number of resources with manager argocd, by any check; FromStatus of them were
	// found only in the status.resources of a local Application.
	Managed    int `json:"managed"`
	FromStatus int `json:"from_status,omitempty"`
}

// NodeSummary aggregates nodes; individual nodes are not inventory items. Instance and capacity
// types are counted from node labels (node.kubernetes.io/instance-type, karpenter.sh/capacity-type
// and the cloud equivalents).
type NodeSummary struct {
	Count          int            `json:"count"`
	Arch           map[string]int `json:"arch,omitempty"`
	InstanceTypes  map[string]int `json:"instance_types,omitempty"`
	CapacityTypes  map[string]int `json:"capacity_types,omitempty"`
	Zones          []string       `json:"zones,omitempty"`
	KubeletVersion []string       `json:"kubelet_versions,omitempty"`
}

// Scope records what the collector was asked to cover.
type Scope struct {
	Kinds []string `json:"kinds"`
	// SkippedKinds are the served kinds left out on purpose (collect --skip-kinds, --no-secrets).
	SkippedKinds []string `json:"skipped_kinds,omitempty"`
	SystemNS     string   `json:"system_ns"`
	TeamKeys     []string `json:"team_keys"`
	// TeamFromNS is the regexp whose first group names a namespace's team from its name
	// (collect --team-from-ns); RequesterIgnore are the openshift.io/requester users never taken
	// as a team (built-in ones included).
	TeamFromNS      string   `json:"team_from_ns,omitempty"`
	RequesterIgnore []string `json:"requester_ignore,omitempty"`
}

// Namespace is the sector of the radial view.
type Namespace struct {
	Name string `json:"name"`
	Team string `json:"team,omitempty"`
	// TeamFrom is the label or annotation key that named the team (openshift.io/requester holds a
	// user name). Empty in snapshots collected before 0.2.
	TeamFrom string    `json:"team_from,omitempty"`
	System   bool      `json:"system,omitempty"`
	Created  time.Time `json:"created"`
}

// Resource is the sanitized projection of one object. It never holds Secret data, env values,
// args, commands or annotations.
type Resource struct {
	ID      string            `json:"id"`
	Group   string            `json:"group"`
	Version string            `json:"version"`
	Kind    string            `json:"kind"`
	Cat     string            `json:"cat"`
	NS      string            `json:"ns,omitempty"`
	Name    string            `json:"name"`
	Created time.Time         `json:"created"`
	Changed time.Time         `json:"changed,omitzero"`
	Manager string            `json:"manager"`
	App     string            `json:"app,omitempty"`
	Team    string            `json:"team,omitempty"`
	Owner   string            `json:"owner,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
	Facts   map[string]any    `json:"facts,omitempty"`
	FP      string            `json:"fp,omitempty"`
	// Platform marks a custom resource of the cluster platform itself (OpenShift cluster operator
	// kinds such as MachineConfig or APIRequestCount): cat stays crd, the page shows it as the
	// platform category, hidden by default.
	Platform bool `json:"platform,omitempty"`
}

// Edge links two resources of the same snapshot.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rel  string `json:"rel"`
}

// Error is a non-fatal collection problem (RBAC, discovery, auth).
type Error struct {
	Kind    string `json:"kind"`
	NS      string `json:"ns,omitempty"`
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
}

// TeamCoverage counts what has a team, outside system namespaces: namespaces, and the resources
// in them (cluster-scoped resources are left out).
type TeamCoverage struct {
	Namespaces         int `json:"namespaces"`
	NamespacesWithTeam int `json:"namespaces_with_team"`
	Resources          int `json:"resources"`
	ResourcesWithTeam  int `json:"resources_with_team"`
}

// TeamCoverage of the snapshot.
func (s *Snapshot) TeamCoverage() TeamCoverage {
	var c TeamCoverage
	system := map[string]bool{}
	for _, n := range s.Namespaces {
		system[n.Name] = n.System
		if !n.System {
			c.Namespaces++
			if n.Team != "" {
				c.NamespacesWithTeam++
			}
		}
	}
	for _, r := range s.Resources {
		if r.NS == "" || system[r.NS] {
			continue
		}
		c.Resources++
		if r.Team != "" {
			c.ResourcesWithTeam++
		}
	}
	return c
}

// ClusterScoped is the namespace segment of IDs for cluster-scoped objects.
const ClusterScoped = "_"

// ID builds the identity key group/Kind/namespace/name used for edges and comparison.
func ID(group, kind, ns, name string) string {
	if group == "" {
		group = "core"
	}
	if ns == "" {
		ns = ClusterScoped
	}
	return group + "/" + kind + "/" + ns + "/" + name
}

// GroupKind returns "group/Kind" with the core group spelled "core".
func GroupKind(group, kind string) string {
	if group == "" {
		group = "core"
	}
	return group + "/" + kind
}

// Normalize sorts every list so that the same cluster state always serializes to the same bytes.
func (s *Snapshot) Normalize() {
	sort.Slice(s.Namespaces, func(i, j int) bool { return s.Namespaces[i].Name < s.Namespaces[j].Name })
	sort.Slice(s.Resources, func(i, j int) bool { return s.Resources[i].ID < s.Resources[j].ID })
	sort.Slice(s.Edges, func(i, j int) bool {
		a, b := s.Edges[i], s.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Rel < b.Rel
	})
	sort.Slice(s.Errors, func(i, j int) bool {
		a, b := s.Errors[i], s.Errors[j]
		return a.Kind+"\x00"+a.NS+"\x00"+a.Reason < b.Kind+"\x00"+b.NS+"\x00"+b.Reason
	})
	sort.Strings(s.Scope.Kinds)
	sort.Strings(s.Scope.SkippedKinds)
	if s.Edges == nil {
		s.Edges = []Edge{}
	}
	if s.Errors == nil {
		s.Errors = []Error{}
	}
}

// WriteGzip writes the snapshot as gzip-compressed JSON.
func WriteGzip(w io.Writer, s *Snapshot) error {
	zw := gzip.NewWriter(w)
	enc := json.NewEncoder(zw)
	if err := enc.Encode(s); err != nil {
		return err
	}
	return zw.Close()
}

// ReadGzip reads a snapshot and refuses versions newer than this build understands.
func ReadGzip(r io.Reader) (*Snapshot, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	defer zr.Close()
	return Read(zr)
}

// Read reads a plain JSON snapshot with the same version check.
func Read(r io.Reader) (*Snapshot, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	if head.SchemaVersion < 1 || head.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("snapshot: schema_version %d not supported (this build reads 1..%d)",
			head.SchemaVersion, SchemaVersion)
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	return &s, nil
}

// HostOf strips scheme and path from a server URL.
func HostOf(server string) string {
	h := server
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.IndexByte(h, '/'); i >= 0 {
		h = h[:i]
	}
	return h
}
