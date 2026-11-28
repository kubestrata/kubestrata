// Package summary condenses one snapshot into the figures a migration plan starts from: what
// exists by category and kind, the nodes, who manages what, the biggest namespaces, what is at end
// of life, what has no expected owner, how much has a team and what Argo CD manages. Resources of
// system namespaces are counted apart, like the default view leaves them out. The result is
// deterministic: the same snapshot gives the same document.
package summary

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/kubestrata/kubestrata/internal/snapshot"
)

// SchemaVersion of the summary document. Consumers must refuse a higher value.
const SchemaVersion = 1

// topNamespaces is how many namespaces the summary lists by size.
const topNamespaces = 10

// Summary of one snapshot.
type Summary struct {
	SchemaVersion int     `json:"schema_version"`
	Cluster       Cluster `json:"cluster"`
	// Resources outside system namespaces (cluster-scoped ones included); SystemResources are left
	// out of every count below.
	Resources       int                   `json:"resources"`
	SystemResources int                   `json:"system_resources"`
	Categories      []Count               `json:"categories"`
	Kinds           []Count               `json:"kinds"`
	Nodes           snapshot.NodeSummary  `json:"nodes"`
	Managers        []Count               `json:"managers"`
	TopNamespaces   []Namespace           `json:"top_namespaces"`
	EndOfLife       []EndOfLife           `json:"end_of_life"`
	Unowned         []Unowned             `json:"unowned"`
	TeamCoverage    snapshot.TeamCoverage `json:"team_coverage"`
	ArgoCD          *snapshot.ArgoCD      `json:"argocd,omitempty"`
	Errors          int                   `json:"errors"`
	SkippedKinds    []string              `json:"skipped_kinds,omitempty"`
}

// Cluster identifies the snapshot.
type Cluster struct {
	ID          string    `json:"id"`
	Context     string    `json:"context"`
	Distro      string    `json:"distro"`
	Version     string    `json:"version"`
	CollectedAt time.Time `json:"collected_at"`
	Tool        string    `json:"tool,omitempty"`
}

// Count is a name and how many resources it has.
type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Namespace is one of the biggest namespaces.
type Namespace struct {
	NS        string `json:"ns"`
	Team      string `json:"team,omitempty"`
	Resources int    `json:"resources"`
}

// EndOfLife groups the resources that share an end-of-life note (facts.end_of_life).
type EndOfLife struct {
	Kind      string   `json:"kind"`
	Note      string   `json:"note"`
	Resources []string `json:"resources"`
}

// Unowned is a pod, ReplicaSet, ReplicationController or Job shown on its own: without an owner
// (orphan) or with an owner that was not collected (owner_missing).
type Unowned struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// Of summarizes a snapshot.
func Of(s *snapshot.Snapshot) *Summary {
	out := &Summary{SchemaVersion: SchemaVersion, Nodes: s.Cluster.Nodes, ArgoCD: s.Cluster.ArgoCD,
		Errors: len(s.Errors), SkippedKinds: s.Scope.SkippedKinds, TeamCoverage: s.TeamCoverage(),
		Cluster: Cluster{ID: s.Cluster.ID, Context: s.Cluster.Context, Distro: s.Cluster.Distro,
			Version: s.Cluster.Version, CollectedAt: s.CollectedAt.UTC(), Tool: s.Tool.Version},
		Categories: []Count{}, Kinds: []Count{}, Managers: []Count{}, TopNamespaces: []Namespace{},
		EndOfLife: []EndOfLife{}, Unowned: []Unowned{}}
	system, team := map[string]bool{}, map[string]string{}
	for _, n := range s.Namespaces {
		system[n.Name], team[n.Name] = n.System, n.Team
	}
	cats, kinds, mgrs, perNS := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	eol := map[[2]string][]string{}
	for _, r := range s.Resources {
		if system[r.NS] {
			out.SystemResources++
			continue
		}
		out.Resources++
		cat := r.Cat
		if r.Platform {
			cat = "platform"
		}
		cats[cat]++
		kinds[snapshot.GroupKind(r.Group, r.Kind)]++
		mgrs[r.Manager]++
		if r.NS != "" {
			perNS[r.NS]++
		}
		if note, ok := r.Facts["end_of_life"].(string); ok && note != "" {
			k := [2]string{snapshot.GroupKind(r.Group, r.Kind), note}
			eol[k] = append(eol[k], r.ID)
		}
		switch {
		case r.Facts["orphan"] == true:
			out.Unowned = append(out.Unowned, Unowned{ID: r.ID, Reason: "orphan"})
		case r.Facts["owner_missing"] == true:
			out.Unowned = append(out.Unowned, Unowned{ID: r.ID, Reason: "owner_missing"})
		}
	}
	out.Categories, out.Kinds, out.Managers = sorted(cats), sorted(kinds), sorted(mgrs)
	for _, c := range sorted(perNS) {
		if len(out.TopNamespaces) == topNamespaces {
			break
		}
		out.TopNamespaces = append(out.TopNamespaces, Namespace{NS: c.Name, Team: team[c.Name], Resources: c.Count})
	}
	for k, ids := range eol {
		sort.Strings(ids)
		out.EndOfLife = append(out.EndOfLife, EndOfLife{Kind: k[0], Note: k[1], Resources: ids})
	}
	sort.Slice(out.EndOfLife, func(i, j int) bool {
		a, b := out.EndOfLife[i], out.EndOfLife[j]
		if len(a.Resources) != len(b.Resources) {
			return len(a.Resources) > len(b.Resources)
		}
		return a.Kind+a.Note < b.Kind+b.Note
	})
	sort.Slice(out.Unowned, func(i, j int) bool { return out.Unowned[i].ID < out.Unowned[j].ID })
	return out
}

// sorted lists counts by count, largest first, then by name.
func sorted(m map[string]int) []Count {
	out := make([]Count, 0, len(m))
	for k, v := range m {
		out = append(out, Count{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Hints are the migration paths of the managers, as the page shows them.
var Hints = map[string]string{
	"argocd": "GitOps: re-target the destination", "flux": "GitOps: re-target the destination",
	"helm": "reinstall the release in the target", "kapp": "deploy the same kapp app to the target",
	"terraform": "IaC: re-target the Terraform code to the new cluster",
	"operator":  "migrate the custom resource, not its children", "kubectl": "applied by hand: export, clean, move to Git",
	"console":  "edited in a web console: review and codify before migrating",
	"platform": "recreated by the target platform, do not migrate", "other": "unknown tool: check managedFields",
	"unknown": "no manager data",
}

// Markdown writes the summary as a Markdown document.
func Markdown(w io.Writer, s *Summary) error {
	var b strings.Builder
	c := s.Cluster
	fmt.Fprintf(&b, "# kubestrata summary: %s\n\n", md(c.Context))
	fmt.Fprintf(&b, "Cluster `%s`, %s %s, collected %s", c.ID, md(c.Distro), md(c.Version), c.CollectedAt.Format(time.RFC3339))
	if c.Tool != "" {
		fmt.Fprintf(&b, " by kubestrata %s", md(c.Tool))
	}
	fmt.Fprintf(&b, ". %d resources outside system namespaces (%d more in system namespaces, left out below).\n", s.Resources,
		s.SystemResources)

	table(&b, "Categories", []string{"Category", "Resources"}, counts(s.Categories))
	table(&b, "Kinds", []string{"Kind", "Resources"}, counts(s.Kinds))

	b.WriteString("\n## Nodes\n\n")
	n := s.Nodes
	fmt.Fprintf(&b, "Nodes: %d", n.Count)
	if len(n.Arch) > 0 {
		fmt.Fprintf(&b, "; architectures: %s", mapText(n.Arch))
	}
	if len(n.InstanceTypes) > 0 {
		fmt.Fprintf(&b, "; instance types: %s", mapText(n.InstanceTypes))
	}
	if len(n.CapacityTypes) > 0 {
		fmt.Fprintf(&b, "; capacity types: %s", mapText(n.CapacityTypes))
	}
	switch {
	case len(n.ZoneCounts) > 0:
		fmt.Fprintf(&b, "; zones: %s", mapText(n.ZoneCounts))
	case len(n.Zones) > 0:
		fmt.Fprintf(&b, "; zones: %s", md(strings.Join(n.Zones, ", ")))
	}
	b.WriteString(".\n")

	var mrows [][]string
	for _, m := range s.Managers {
		mrows = append(mrows, []string{md(m.Name), fmt.Sprint(m.Count), Hints[m.Name]})
	}
	table(&b, "Managers", []string{"Manager", "Resources", "Migration path"}, mrows)

	var nrows [][]string
	for _, ns := range s.TopNamespaces {
		nrows = append(nrows, []string{md(ns.NS), md(ns.Team), fmt.Sprint(ns.Resources)})
	}
	table(&b, fmt.Sprintf("Top namespaces (by resources, up to %d)", topNamespaces), []string{"Namespace", "Team", "Resources"}, nrows)

	var erows [][]string
	for _, e := range s.EndOfLife {
		erows = append(erows, []string{md(e.Kind), fmt.Sprint(len(e.Resources)), md(e.Note)})
	}
	table(&b, "Deprecated or end of life", []string{"Kind", "Resources", "Note"}, erows)

	var urows [][]string
	for _, u := range s.Unowned {
		why := "no owner"
		if u.Reason == "owner_missing" {
			why = "owner not collected"
		}
		urows = append(urows, []string{md(u.ID), why})
	}
	table(&b, "Without an expected owner", []string{"Resource", "Why"}, urows)

	t := s.TeamCoverage
	fmt.Fprintf(&b, "\n## Teams\n\n%d of %d namespaces have a team; %d of %d of their resources (%s).\n",
		t.NamespacesWithTeam, t.Namespaces, t.ResourcesWithTeam, t.Resources, percent(t.ResourcesWithTeam, t.Resources))

	if a := s.ArgoCD; a != nil {
		b.WriteString("\n## Argo CD\n\n")
		if a.Managed == 0 {
			fmt.Fprintf(&b, "Argo CD installed, no managed objects found (%d Applications, %d deploying to other clusters).\n",
				a.Applications, a.Remote)
		} else {
			fmt.Fprintf(&b, "%d resources managed by Argo CD, %d of them found only in Application status (%d Applications, %d deploying to other clusters).\n",
				a.Managed, a.FromStatus, a.Applications, a.Remote)
		}
	}

	fmt.Fprintf(&b, "\n## Collection\n\nCollection errors: %d", s.Errors)
	if len(s.SkippedKinds) > 0 {
		fmt.Fprintf(&b, "; kinds left out on purpose: %s", md(strings.Join(s.SkippedKinds, ", ")))
	}
	b.WriteString(".\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func counts(cs []Count) [][]string {
	rows := make([][]string, 0, len(cs))
	for _, c := range cs {
		rows = append(rows, []string{md(c.Name), fmt.Sprint(c.Count)})
	}
	return rows
}

func table(b *strings.Builder, title string, head []string, rows [][]string) {
	fmt.Fprintf(b, "\n## %s\n\n", title)
	if len(rows) == 0 {
		b.WriteString("None.\n")
		return
	}
	b.WriteString("| " + strings.Join(head, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat("---|", len(head)) + "\n")
	for _, r := range rows {
		b.WriteString("| " + strings.Join(r, " | ") + " |\n")
	}
}

// mapText writes counts as "a: 2, b: 1", by key.
func mapText(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s: %d", md(k), m[k])
	}
	return strings.Join(parts, ", ")
}

func percent(n, total int) string {
	if total == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", math.Round(float64(n)*100/float64(total)))
}

// md keeps cluster strings from breaking a table or adding markup: pipes, backticks and line
// breaks are escaped or removed.
func md(s string) string {
	return strings.NewReplacer("|", `\|`, "`", "'", "\n", " ", "\r", " ", "<", "&lt;", ">", "&gt;").Replace(s)
}
