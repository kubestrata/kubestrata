package kube

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLoadContexts(t *testing.T) {
	got, current, err := LoadContexts("testdata/kubeconfig.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if current != "lab-origin" {
		t.Fatalf("current = %q", current)
	}
	want := []Context{
		{Name: "dangling", Cluster: "missing", User: "missing"},
		{Name: "eks-target", Cluster: "target", Server: "https://ABCDEF.gr7.us-east-1.eks.amazonaws.com",
			User: "eks-user", AuthExec: "aws"},
		{Name: "lab-origin", Cluster: "origin", Server: "https://127.0.0.1:6443", User: "origin-admin",
			Namespace: "payments", Current: true},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("context %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

func TestLoadContextsNeverLeaksCredentials(t *testing.T) {
	got, _, err := LoadContexts("testdata/kubeconfig.yaml")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	for _, s := range []string{"fake-token-must-never-appear", "get-token", "--cluster-name"} {
		if strings.Contains(string(b), s) {
			t.Errorf("output leaks %q", s)
		}
	}
}

func TestLoadContextsMissingFile(t *testing.T) {
	if _, _, err := LoadContexts("testdata/does-not-exist.yaml"); err == nil {
		t.Fatal("want error for missing explicit kubeconfig")
	}
}
