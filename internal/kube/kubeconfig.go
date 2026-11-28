// Package kube loads kubeconfig contexts and talks to clusters, read-only.
package kube

import (
	"fmt"
	"path/filepath"
	"sort"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Context is one kubeconfig context as kubestrata shows it. It never carries credentials:
// for exec-based auth only the plugin command name is kept (e.g. "aws", "oc").
type Context struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	Server    string `json:"server"`
	User      string `json:"user"`
	Namespace string `json:"namespace,omitempty"`
	AuthExec  string `json:"auth_exec,omitempty"`
	// Auth is how the user authenticates: exec, token, token-file, client-certificate,
	// auth-provider or basic; empty when the user has none of them or is missing.
	Auth string `json:"auth,omitempty"`
	// InsecureSkipTLSVerify: the cluster's certificate is not checked (insecure-skip-tls-verify),
	// so anyone on the path can pose as the API server and receive the credential.
	InsecureSkipTLSVerify bool `json:"insecure_skip_tls_verify,omitempty"`
	// StaticToken: the user holds a bearer token (token or token-file) instead of an exec plugin
	// or a client certificate. The token itself is never read into this struct.
	StaticToken bool `json:"static_token,omitempty"`
	Current     bool `json:"current"`
}

// authOf names how a kubeconfig user authenticates, strongest hint first.
func authOf(u *clientcmdapi.AuthInfo) string {
	switch {
	case u.Exec != nil:
		return "exec"
	case u.AuthProvider != nil:
		return "auth-provider"
	case u.ClientCertificate != "" || len(u.ClientCertificateData) > 0:
		return "client-certificate"
	case u.Token != "":
		return "token"
	case u.TokenFile != "":
		return "token-file"
	case u.Username != "" || u.Password != "":
		return "basic"
	}
	return ""
}

// LoadingRules returns client-go's default loading rules (KUBECONFIG with several files, then
// ~/.kube/config), or only the explicit path when one is given.
func LoadingRules(explicitPath string) *clientcmd.ClientConfigLoadingRules {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if explicitPath != "" {
		rules.ExplicitPath = explicitPath
	}
	return rules
}

// LoadContexts returns the contexts of the merged kubeconfig sorted by name, and the current
// context name. A context pointing to a missing cluster or user keeps empty fields instead of
// failing the whole listing.
func LoadContexts(explicitPath string) ([]Context, string, error) {
	cfg, err := LoadingRules(explicitPath).Load()
	if err != nil {
		return nil, "", fmt.Errorf("load kubeconfig: %w", err)
	}
	out := make([]Context, 0, len(cfg.Contexts))
	for name, c := range cfg.Contexts {
		ctx := Context{Name: name, Cluster: c.Cluster, User: c.AuthInfo, Namespace: c.Namespace,
			Current: name == cfg.CurrentContext}
		if cl, ok := cfg.Clusters[c.Cluster]; ok {
			ctx.Server = cl.Server
			ctx.InsecureSkipTLSVerify = cl.InsecureSkipTLSVerify
		}
		if u, ok := cfg.AuthInfos[c.AuthInfo]; ok {
			if u.Exec != nil {
				ctx.AuthExec = filepath.Base(u.Exec.Command)
			}
			ctx.Auth = authOf(u)
			ctx.StaticToken = ctx.Auth == "token" || ctx.Auth == "token-file"
		}
		out = append(out, ctx)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, cfg.CurrentContext, nil
}
