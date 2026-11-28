package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A bad --skip-kinds entry stops collect before any cluster is contacted.
func TestCollectSkipKindsValidated(t *testing.T) {
	kc := kubeconfig(t, "https://127.0.0.1:1", "source")
	for _, args := range [][]string{
		{"--skip-kinds", "a/b/c"},
		{"--no-secrets", "--skip-kinds", "core/"},
	} {
		a := append([]string{"collect", "--kubeconfig", kc, "--cache-dir", t.TempDir(), "-c", "source"}, args...)
		if _, err := run(t, a...); err == nil || !strings.Contains(err.Error(), "skip kinds") {
			t.Errorf("%v: %v", args, err)
		}
	}
}

// collect reports the team coverage of each snapshot, outside system namespaces (K17).
func TestCollectTeamCoverage(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	team := deployment("spark", "api", "api:1", now)
	team.Team = "team-data"
	putSnap(t, dir, "aaaaaaaaaaaa", "source", now, team, deployment("spark", "worker", "worker:1", now),
		configMap("kube-system", "x", "sha256:x", now))
	kc := kubeconfig(t, "https://127.0.0.1:1", "source")
	out, err := run(t, "collect", "--cache-dir", dir, "--kubeconfig", kc, "-c", "source", "--format", "json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var doc collectDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Results) != 1 || doc.Results[0].TeamCoverage == nil {
		t.Fatalf("%v %s", err, out)
	}
	if c := *doc.Results[0].TeamCoverage; c.Namespaces != 1 || c.NamespacesWithTeam != 1 || c.Resources != 2 || c.ResourcesWithTeam != 1 {
		t.Errorf("team coverage = %+v", c)
	}
	text, err := run(t, "collect", "--cache-dir", dir, "--kubeconfig", kc, "-c", "source")
	if err != nil || !strings.Contains(text, "TEAMS") || !strings.Contains(text, "1/1 ns, 50% res") {
		t.Errorf("text: %v\n%s", err, text)
	}
}

// summary reads the cache only; --redact masks the requester team and the oc login host.
func TestSummaryCommand(t *testing.T) {
	dir, kc := seedOpenShift(t)
	out, err := run(t, "summary", "-c", ocpSource, "--cache-dir", dir, "--kubeconfig", kc, "--redact")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"# kubestrata summary: ns-a/api-redacted:6443/user-a", "## Managers", "route.openshift.io/Route | 1",
		"| ns-a | redacted-1 | 1 |"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary misses %q:\n%s", want, out)
		}
	}
	assertNoOCPLeak(t, "summary", []byte(out))
	js, err := run(t, "summary", "-c", ocpSource, "--cache-dir", dir, "--kubeconfig", kc, "--format", "json")
	var doc struct {
		SchemaVersion int `json:"schema_version"`
		Resources     int `json:"resources"`
	}
	if err != nil || json.Unmarshal([]byte(js), &doc) != nil || doc.SchemaVersion != 1 || doc.Resources != 1 {
		t.Errorf("summary json: %v %+v\n%s", err, doc, js)
	}
	if _, err := run(t, "summary", "-c", "missing", "--cache-dir", dir, "--kubeconfig", kc); err == nil {
		t.Error("summary of an uncached context")
	}
}
