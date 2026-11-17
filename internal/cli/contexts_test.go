package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestContextsJSON(t *testing.T) {
	out, err := run(t, "contexts", "--kubeconfig", "../kube/testdata/kubeconfig.yaml", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var doc contextsDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if doc.SchemaVersion != SchemaVersion || doc.Current != "lab-origin" || len(doc.Contexts) != 3 {
		t.Fatalf("unexpected doc: %+v", doc)
	}
}

func TestContextsText(t *testing.T) {
	out, err := run(t, "contexts", "--kubeconfig", "../kube/testdata/kubeconfig.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"CONTEXT", "lab-origin", "eks-target", "aws"} {
		if !strings.Contains(out, s) {
			t.Errorf("text output misses %q:\n%s", s, out)
		}
	}
}

func TestBadFormat(t *testing.T) {
	if _, err := run(t, "contexts", "--kubeconfig", "../kube/testdata/kubeconfig.yaml", "--format", "yaml"); err == nil {
		t.Fatal("want error for --format yaml")
	}
}
