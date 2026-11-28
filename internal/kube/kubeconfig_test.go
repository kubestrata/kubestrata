package kube

import (
	"encoding/json"
	"strings"
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
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
			User: "eks-user", AuthExec: "aws", Auth: "exec"},
		{Name: "lab-origin", Cluster: "origin", Server: "https://127.0.0.1:6443", User: "origin-admin",
			Namespace: "payments", Current: true, Auth: "token", InsecureSkipTLSVerify: true, StaticToken: true},
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

func TestAuthOf(t *testing.T) {
	for want, u := range map[string]*clientcmdapi.AuthInfo{
		"exec":               {Exec: &clientcmdapi.ExecConfig{Command: "oc"}, Token: "x"},
		"auth-provider":      {AuthProvider: &clientcmdapi.AuthProviderConfig{Name: "oidc"}},
		"client-certificate": {ClientCertificateData: []byte("cert"), ClientKeyData: []byte("key")},
		"token":              {Token: "not-a-real-token"},
		"token-file":         {TokenFile: "/var/run/secrets/token"},
		"basic":              {Username: "user-a", Password: "x"},
		"":                   {},
	} {
		if got := authOf(u); got != want {
			t.Errorf("%+v: auth %q, want %q", u, got, want)
		}
	}
}
