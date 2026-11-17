// Package render writes the offline site for one or more cached snapshots:
//
//	index.html            the view, CSS and JS inline, no network
//	data/<cluster_id>.js  window.KUBESTRATA["<id>"] = <snapshot>
//	json/<cluster_id>.json the same snapshot for machines
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
	"sort"
	"strings"

	"github.com/leosavio/kubestrata/internal/snapshot"
	"github.com/leosavio/kubestrata/web"
)

// ViewSchemaVersion is the version of the view config embedded in the page.
const ViewSchemaVersion = 1

const (
	dirMode  = 0o700
	fileMode = 0o600
)

// Scales the view offers.
var Scales = []string{"5d", "10d", "30d", "log", "custom"}

// Options of a render.
type Options struct {
	Mode    string // single (compare comes next)
	Scale   string
	Axis    string
	Rings   []int
	Version string
}

type viewConfig struct {
	SchemaVersion int      `json:"schema_version"`
	Mode          string   `json:"mode"`
	Clusters      []string `json:"clusters"`
	Scale         string   `json:"scale"`
	Axis          string   `json:"axis"`
	Rings         []int    `json:"rings,omitempty"`
	Version       string   `json:"version"`
}

var idRE = regexp.MustCompile(`^[0-9a-f]{12}$`)

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
	case !ok:
		return fmt.Errorf("--scale must be one of %s", strings.Join(Scales, ", "))
	case o.Scale == "custom" && len(o.Rings) == 0:
		return fmt.Errorf("--scale custom needs --rings")
	case o.Axis != "created" && o.Axis != "changed":
		return fmt.Errorf("--axis must be created or changed")
	}
	if len(o.Rings) > 0 {
		if !sort.IntsAreSorted(o.Rings) || o.Rings[0] <= 0 {
			return fmt.Errorf("--rings must be positive days in ascending order")
		}
		if o.Scale != "custom" {
			o.Scale = "custom"
		}
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
	for _, sub := range []string{"", "data", "json"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), dirMode); err != nil {
			return "", err
		}
	}
	cfg := viewConfig{SchemaVersion: ViewSchemaVersion, Mode: o.Mode, Scale: o.Scale, Axis: o.Axis,
		Rings: o.Rings, Version: o.Version}
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
		raw, err := json.Marshal(s) // escapes <, >, & and U+2028/2029: safe inside a script
		if err != nil {
			return "", err
		}
		js := "window.KUBESTRATA=window.KUBESTRATA||{};window.KUBESTRATA[\"" + id + "\"]=" + string(raw) + ";\n"
		if err := writeFile(filepath.Join(dir, "data", id+".js"), []byte(js)); err != nil {
			return "", err
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err != nil {
			return "", err
		}
		pretty.WriteByte('\n')
		if err := writeFile(filepath.Join(dir, "json", id+".json"), pretty.Bytes()); err != nil {
			return "", err
		}
		fmt.Fprintf(&scripts, "<script src=\"data/%s.js\"></script>\n", id)
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
