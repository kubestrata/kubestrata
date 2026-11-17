package cache

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/leosavio/kubestrata/internal/snapshot"
)

func snap(id, ctx string, at time.Time) *snapshot.Snapshot {
	s := &snapshot.Snapshot{SchemaVersion: snapshot.SchemaVersion, CollectedAt: at,
		Cluster: snapshot.Cluster{ID: id, Context: ctx}}
	s.Normalize()
	return s
}

func TestPutLatestLoad(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	if _, err := c.Put(snap("abc", "lab", t0)); err != nil {
		t.Fatal(err)
	}
	e, err := c.Put(snap("abc", "lab", t0.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.Latest("lab")
	if err != nil || !ok || got.Path != e.Path {
		t.Fatalf("latest = %+v ok=%v err=%v", got, ok, err)
	}
	s, err := c.Load(got)
	if err != nil || !s.CollectedAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("load = %+v err=%v", s, err)
	}
	fi, _ := os.Stat(e.Path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("snapshot mode = %v", fi.Mode().Perm())
	}
	di, _ := os.Stat(c.Dir)
	if di.Mode().Perm() != 0o700 {
		t.Errorf("cache dir mode = %v", di.Mode().Perm())
	}
}

func TestPrune(t *testing.T) {
	c, _ := Open(t.TempDir())
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		if _, err := c.Put(snap("abc", "lab", t0.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := c.Prune(2)
	if err != nil || len(removed) != 2 {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	all, _ := c.List()
	if len(all) != 2 || !all[0].CollectedAt.Equal(t0.Add(3*time.Hour)) {
		t.Fatalf("after prune: %+v", all)
	}
}

func TestReadVersionGate(t *testing.T) {
	if _, err := snapshot.Read(strings.NewReader(`{"schema_version": 2}`)); err == nil ||
		!strings.Contains(err.Error(), "not supported") {
		t.Fatalf("want refusal of schema_version 2, got %v", err)
	}
	if _, err := snapshot.Read(strings.NewReader(`{"schema_version": 1}`)); err != nil {
		t.Fatalf("schema_version 1 must load: %v", err)
	}
}
