// Package cache stores snapshots on local disk, outside any repository, readable only by the
// user. Layout:
//
//	<dir>/clusters/<cluster_id>/<collected_at>.json.gz   one file per collection
//	<dir>/contexts.json                                  context name → cluster id, last collection
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/leosavio/kubestrata/internal/snapshot"
)

const (
	dirMode   = 0o700
	fileMode  = 0o600
	stampFmt  = "20060102T150405Z"
	indexName = "contexts.json"
	indexVer  = 1
)

// Cache is a snapshot store rooted at Dir.
type Cache struct{ Dir string }

// Entry is one cached snapshot.
type Entry struct {
	ClusterID   string    `json:"cluster_id"`
	Context     string    `json:"context"`
	Distro      string    `json:"distro,omitempty"`
	Version     string    `json:"version,omitempty"`
	CollectedAt time.Time `json:"collected_at"`
	// Stamp names the snapshot file (20261120T144809Z); `kubestrata diff --from/--to` takes it.
	Stamp     string `json:"stamp"`
	Path      string `json:"path"`
	Resources int    `json:"resources"`
	Errors    int    `json:"errors"`
}

// Stamp formats a collection time the way snapshot files are named.
func Stamp(t time.Time) string { return t.UTC().Format(stampFmt) }

// ParseStamp reads a stamp (20261120T144809Z) or an RFC 3339 time.
func ParseStamp(s string) (time.Time, error) {
	if t, err := time.Parse(stampFmt, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is neither a stamp like 20261120T144809Z nor an RFC 3339 time", s)
	}
	return t.UTC(), nil
}

type index struct {
	SchemaVersion int              `json:"schema_version"`
	Contexts      map[string]Entry `json:"contexts"`
}

// DefaultDir is $KUBESTRATA_CACHE_DIR or <user cache dir>/kubestrata (~/Library/Caches on macOS).
func DefaultDir() (string, error) {
	if d := os.Getenv("KUBESTRATA_CACHE_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "kubestrata"), nil
}

// Open creates the cache directory with owner-only permissions.
func Open(dir string) (*Cache, error) {
	if err := os.MkdirAll(filepath.Join(dir, "clusters"), dirMode); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return nil, err
	}
	return &Cache{Dir: dir}, nil
}

// Put writes the snapshot atomically and points its context at it.
func (c *Cache) Put(s *snapshot.Snapshot) (Entry, error) {
	if s.Cluster.ID == "" {
		return Entry{}, errors.New("cache: snapshot without cluster id")
	}
	cdir := filepath.Join(c.Dir, "clusters", s.Cluster.ID)
	if err := os.MkdirAll(cdir, dirMode); err != nil {
		return Entry{}, err
	}
	stamp := Stamp(s.CollectedAt)
	p := filepath.Join(cdir, stamp+".json.gz")
	if err := writeAtomic(p, func(f *os.File) error { return snapshot.WriteGzip(f, s) }); err != nil {
		return Entry{}, err
	}
	e := Entry{ClusterID: s.Cluster.ID, Context: s.Cluster.Context, Distro: s.Cluster.Distro,
		Version: s.Cluster.Version, CollectedAt: s.CollectedAt, Stamp: stamp, Path: p,
		Resources: len(s.Resources), Errors: len(s.Errors)}
	idx, err := c.readIndex()
	if err != nil {
		return Entry{}, err
	}
	idx.Contexts[s.Cluster.Context] = e
	return e, c.writeIndex(idx)
}

// Latest returns the newest entry recorded for a context.
func (c *Cache) Latest(context string) (Entry, bool, error) {
	idx, err := c.readIndex()
	if err != nil {
		return Entry{}, false, err
	}
	e, ok := idx.Contexts[context]
	if ok {
		if _, err := os.Stat(e.Path); err != nil {
			return Entry{}, false, nil
		}
		if e.Stamp == "" {
			e.Stamp = Stamp(e.CollectedAt) // index written before stamps were recorded
		}
	}
	return e, ok, nil
}

// History returns the cached snapshots of one cluster, newest first.
func (c *Cache) History(clusterID string) ([]Entry, error) {
	all, err := c.List()
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range all {
		if e.ClusterID == clusterID {
			out = append(out, e)
		}
	}
	return out, nil
}

// Load reads a cached snapshot.
func (c *Cache) Load(e Entry) (*snapshot.Snapshot, error) {
	f, err := os.Open(e.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return snapshot.ReadGzip(f)
}

// List returns every cached snapshot, newest first.
func (c *Cache) List() ([]Entry, error) {
	idx, err := c.readIndex()
	if err != nil {
		return nil, err
	}
	ctxOf := map[string]string{}
	for name, e := range idx.Contexts {
		ctxOf[e.ClusterID] = name
	}
	var out []Entry
	dirs, err := os.ReadDir(filepath.Join(c.Dir, "clusters"))
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(c.Dir, "clusters", d.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			name := f.Name()
			if !strings.HasSuffix(name, ".json.gz") {
				continue
			}
			stamp := strings.TrimSuffix(name, ".json.gz")
			t, err := time.Parse(stampFmt, stamp)
			if err != nil {
				continue
			}
			out = append(out, Entry{ClusterID: d.Name(), Context: ctxOf[d.Name()], CollectedAt: t, Stamp: stamp,
				Path: filepath.Join(c.Dir, "clusters", d.Name(), name)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CollectedAt.Equal(out[j].CollectedAt) {
			return out[i].CollectedAt.After(out[j].CollectedAt)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// Prune keeps the newest `keep` snapshots per cluster and returns the removed paths.
func (c *Cache) Prune(keep int) ([]string, error) {
	if keep < 1 {
		return nil, fmt.Errorf("cache: keep must be ≥ 1")
	}
	all, err := c.List()
	if err != nil {
		return nil, err
	}
	seen := map[string]int{}
	var removed []string
	for _, e := range all {
		seen[e.ClusterID]++
		if seen[e.ClusterID] > keep {
			if err := os.Remove(e.Path); err != nil {
				return removed, err
			}
			removed = append(removed, e.Path)
		}
	}
	return removed, nil
}

func (c *Cache) readIndex() (*index, error) {
	idx := &index{SchemaVersion: indexVer, Contexts: map[string]Entry{}}
	b, err := os.ReadFile(filepath.Join(c.Dir, indexName))
	if errors.Is(err, os.ErrNotExist) {
		return idx, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, idx); err != nil {
		return nil, fmt.Errorf("cache index: %w", err)
	}
	if idx.SchemaVersion > indexVer {
		return nil, fmt.Errorf("cache index: schema_version %d not supported", idx.SchemaVersion)
	}
	if idx.Contexts == nil {
		idx.Contexts = map[string]Entry{}
	}
	return idx, nil
}

func (c *Cache) writeIndex(idx *index) error {
	return writeAtomic(filepath.Join(c.Dir, indexName), func(f *os.File) error {
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(idx)
	})
}

func writeAtomic(p string, write func(*os.File) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return err
	}
	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
