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

// contexts flags a cluster without TLS verification and a static token, never the token (K19).
func TestContextsHygiene(t *testing.T) {
	text, err := run(t, "contexts", "--kubeconfig", "../kube/testdata/kubeconfig.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"NOTES", "TLS not verified, static token", "exec: aws", "Kubeconfig hygiene"} {
		if !strings.Contains(text, s) {
			t.Errorf("text output misses %q:\n%s", s, text)
		}
	}
	js, err := run(t, "contexts", "--kubeconfig", "../kube/testdata/kubeconfig.yaml", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var doc contextsDoc
	if err := json.Unmarshal([]byte(js), &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range doc.Contexts {
		switch c.Name {
		case "lab-origin":
			if !c.InsecureSkipTLSVerify || !c.StaticToken || c.Auth != "token" {
				t.Errorf("lab-origin: %+v", c)
			}
		case "eks-target":
			if c.InsecureSkipTLSVerify || c.StaticToken || c.Auth != "exec" {
				t.Errorf("eks-target: %+v", c)
			}
		}
	}
	for _, out := range []string{text, js} {
		if strings.Contains(out, "fake-token-must-never-appear") {
			t.Error("contexts printed the token")
		}
	}
}
