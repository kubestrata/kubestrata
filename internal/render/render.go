// Package render writes the offline site for one or more cached snapshots:
//
//	index.html            the view, CSS and JS inline, no network
//	data/<cluster_id>.js  window.KUBESTRATA["<id>"] = <snapshot>
//	json/<cluster_id>.json the same snapshot for machines
//	data/compare.js       window.KUBESTRATA_COMPARE = <comparison> (compare mode only)
//	json/compare.json     the same comparison for machines (compare mode only)
//
// Output depends only on the snapshots and the options: the same input gives the same bytes.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/kubestrata/kubestrata/internal/compare"
	"github.com/kubestrata/kubestrata/internal/snapshot"
	"github.com/kubestrata/kubestrata/web"
)

// ViewSchemaVersion is the version of the view config embedded in the page.
const ViewSchemaVersion = 1

const (
	dirMode  = 0o700
	fileMode = 0o600
)

// Scales the view offers: 12 linear rings of 30 minutes, 1 hour, 5, 10 or 30 days (plus a
// compressed outer band), years (30d to 10y, for long-lived clusters), the log scale, auto (rings
// at quantiles of the resource ages, see AutoRings) and custom rings.
var Scales = []string{"30m", "1h", "5d", "10d", "30d", "years", "log", "auto", "custom"}

// maxRingMinutes caps a custom ring at 100 years.
const maxRingMinutes = 100 * 365 * 24 * 60

// Options of a render.
type Options struct {
	Mode  string // single, or compare (two snapshots: source then target)
	Scale string
	Axis  string
	// RingMinutes are the custom rings in minutes, ascending; ParseRings reads them from --rings.
	RingMinutes []int
	// ShowSystem starts the page with the system namespaces shown (the system toggle on).
	ShowSystem bool
	Version    string
	Compare    *compare.Result // compare mode: the comparison of the two snapshots
}

type viewConfig struct {
	SchemaVersion int      `json:"schema_version"`
	Mode          string   `json:"mode"`
	Clusters      []string `json:"clusters"`
	Scale         string   `json:"scale"`
	Axis          string   `json:"axis"`
	RingMinutes   []int    `json:"ring_minutes,omitempty"`
	// AutoRingMinutes are the rings of the auto scale per axis (created, changed), in minutes.
	AutoRingMinutes map[string][]int `json:"auto_ring_minutes,omitempty"`
	ShowSystem      bool             `json:"show_system,omitempty"`
	Version         string           `json:"version"`
}

var idRE = regexp.MustCompile(`^[0-9a-f]{12}$`)

// ParseRings reads the --rings values: a number with the suffix m (minutes), h (hours), d (days)
// or y (years of 365 days); a bare number is days, so "5,15,30" keeps its meaning. It returns
// minutes.
func ParseRings(values []string) ([]int, error) {
	out := make([]int, 0, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		unit := 24 * 60
		switch {
		case strings.HasSuffix(v, "m"):
			unit, v = 1, strings.TrimSuffix(v, "m")
		case strings.HasSuffix(v, "h"):
			unit, v = 60, strings.TrimSuffix(v, "h")
		case strings.HasSuffix(v, "d"):
			v = strings.TrimSuffix(v, "d")
		case strings.HasSuffix(v, "y"):
			unit, v = 365*24*60, strings.TrimSuffix(v, "y")
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > maxRingMinutes/unit {
			return nil, fmt.Errorf("--rings: %q is not a positive whole number of minutes (m), hours (h), days (d, the default) or years (y) up to 100 years", raw)
		}
		out = append(out, n*unit)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// Validate checks the options before anything is written.
func (o *Options) Validate() error {
	if o.Mode == "" {
		o.Mode = "single"
	}
	if o.Scale == "" {
		o.Scale = "30d"
	}
	if o.Axis == "" {
		o.Axis = "created"
	}
	ok := false
	for _, s := range Scales {
		ok = ok || s == o.Scale
	}
	switch {
	case o.Mode != "single" && o.Mode != "compare":
		return fmt.Errorf("render: mode must be single or compare, got %q", o.Mode)
	case !ok:
		return fmt.Errorf("--scale must be one of %s", strings.Join(Scales, ", "))
	case o.Scale == "custom" && len(o.RingMinutes) == 0:
		return fmt.Errorf("--scale custom needs --rings")
	case o.Axis != "created" && o.Axis != "changed":
		return fmt.Errorf("--axis must be created or changed")
	}
	if len(o.RingMinutes) > 0 {
		for i, r := range o.RingMinutes {
			if r <= 0 || r > maxRingMinutes || (i > 0 && r <= o.RingMinutes[i-1]) {
				return fmt.Errorf("--rings must be positive and strictly ascending (e.g. 30m,2h,1d,7d or 5,15,30 in days)")
			}
		}
		o.Scale = "custom"
	}
	return nil
}

// Write renders the site into dir and returns the path of index.html.
func Write(dir string, snaps []*snapshot.Snapshot, o Options) (string, error) {
	if err := o.Validate(); err != nil {
		return "", err
	}
	if len(snaps) == 0 {
		return "", fmt.Errorf("render: no snapshots")
	}
	if o.Mode == "compare" {
		switch {
		case len(snaps) != 2 || o.Compare == nil:
			return "", fmt.Errorf("render: compare mode needs two snapshots and their comparison")
		case snaps[0].Cluster.ID == snaps[1].Cluster.ID:
			return "", fmt.Errorf("render: source and target are the same cluster (%s)", snaps[0].Cluster.ID)
		case o.Compare.Source.ClusterID != snaps[0].Cluster.ID || o.Compare.Target.ClusterID != snaps[1].Cluster.ID:
			return "", fmt.Errorf("render: the comparison does not match the snapshots")
		}
	}
	for _, sub := range []string{"", "data", "json"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), dirMode); err != nil {
			return "", err
		}
	}
	cfg := viewConfig{SchemaVersion: ViewSchemaVersion, Mode: o.Mode, Scale: o.Scale, Axis: o.Axis,
		RingMinutes: o.RingMinutes, Version: o.Version, AutoRingMinutes: map[string][]int{}, ShowSystem: o.ShowSystem}
	for _, axis := range []string{"created", "changed"} {
		if r := AutoRings(snaps, axis); len(r) > 0 {
			cfg.AutoRingMinutes[axis] = r
		}
	}
	var scripts strings.Builder
	seen := map[string]bool{}
	for _, s := range snaps {
		id := s.Cluster.ID
		if !idRE.MatchString(id) {
			return "", fmt.Errorf("render: unexpected cluster id %q", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		cfg.Clusters = append(cfg.Clusters, id)
		js := "window.KUBESTRATA=window.KUBESTRATA||{};window.KUBESTRATA[\"" + id + "\"]="
		if err := writeDoc(dir, id, js, s); err != nil {
			return "", err
		}
		fmt.Fprintf(&scripts, "<script src=\"data/%s.js\"></script>\n", id)
	}
	if o.Mode == "compare" {
		if err := writeDoc(dir, "compare", "window.KUBESTRATA_COMPARE=", o.Compare); err != nil {
			return "", err
		}
		scripts.WriteString("<script src=\"data/compare.js\"></script>\n")
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	page := strings.NewReplacer(
		"__VERSION__", htmlAttr(o.Version),
		"__CSS__", web.CSS,
		"__CONFIG__", string(cfgJSON),
		"__DATA_SCRIPTS__", scripts.String(),
		"__JS__", web.JS,
	).Replace(web.Index)
	index := filepath.Join(dir, "index.html")
	return index, writeFile(index, []byte(page))
}

// writeDoc writes v as data/<name>.js (a script assigning it after jsPrefix) and as indented
// json/<name>.json.
func writeDoc(dir, name, jsPrefix string, v any) error {
	raw, err := json.Marshal(v) // escapes <, >, & and U+2028/2029: safe inside a script
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "data", name+".js"), []byte(jsPrefix+string(raw)+";\n")); err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return err
	}
	pretty.WriteByte('\n')
	return writeFile(filepath.Join(dir, "json", name+".json"), pretty.Bytes())
}

func htmlAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "\"", "&quot;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func writeFile(p string, b []byte) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, fileMode); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
