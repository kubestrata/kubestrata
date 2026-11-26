package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubestrata/kubestrata/internal/collect"
)

const pageSize = 500

// ClusterSource implements collect.Source with list/get calls only.
type ClusterSource struct {
	server string
	dyn    dynamic.Interface
	disc   *discovery.DiscoveryClient
}

// Open builds a read-only client for one kubeconfig context.
func Open(kubeconfig, contextName, userAgent string) (*ClusterSource, error) {
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(LoadingRules(kubeconfig),
		&clientcmd.ConfigOverrides{CurrentContext: contextName})
	cfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", contextName, err)
	}
	cfg.QPS = 20
	cfg.Burst = 40
	cfg.Timeout = 60 * time.Second
	cfg.UserAgent = userAgent
	cfg.WarningHandler = rest.NoWarnings{}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &ClusterSource{server: cfg.Host, dyn: dyn, disc: disc}, nil
}

// Server returns the API server URL.
func (c *ClusterSource) Server() string { return c.server }

// ServerVersion returns the git version of the API server.
func (c *ClusterSource) ServerVersion(context.Context) (string, error) {
	v, err := c.disc.ServerVersion()
	if err != nil {
		return "", err
	}
	return v.GitVersion, nil
}

// KubeSystemUID is the stable half of the cluster identity.
func (c *ClusterSource) KubeSystemUID(ctx context.Context) (string, error) {
	u, err := c.dyn.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).
		Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return string(u.GetUID()), nil
}

// Discover lists the preferred version of every list-able resource and marks custom resources.
func (c *ClusterSource) Discover(ctx context.Context) ([]collect.APIResource, []error) {
	var errs []error
	lists, err := c.disc.ServerPreferredResources()
	if err != nil {
		if !discovery.IsGroupDiscoveryFailedError(err) {
			return nil, []error{err}
		}
		errs = append(errs, err)
	}
	crds := map[string]bool{}
	crdList, err := c.list(ctx, schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1",
		Resource: "customresourcedefinitions"})
	if err != nil {
		errs = append(errs, fmt.Errorf("list CRDs: %w", err))
	}
	for _, u := range crdList {
		g, _, _ := unstructured.NestedString(u.Object, "spec", "group")
		p, _, _ := unstructured.NestedString(u.Object, "spec", "names", "plural")
		crds[g+"/"+p] = true
	}
	var out []collect.APIResource
	for _, l := range lists {
		gv, err := schema.ParseGroupVersion(l.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range l.APIResources {
			if strings.Contains(r.Name, "/") || !hasVerb(r.Verbs, "list") {
				continue
			}
			out = append(out, collect.APIResource{Group: gv.Group, Version: gv.Version, Resource: r.Name,
				Kind: r.Kind, Namespaced: r.Namespaced, CRD: crds[gv.Group+"/"+r.Name]})
		}
	}
	return out, errs
}

func hasVerb(verbs []string, v string) bool {
	for _, x := range verbs {
		if x == v {
			return true
		}
	}
	return false
}

// List returns every object of a resource across namespaces, paginated.
func (c *ClusterSource) List(ctx context.Context, r collect.APIResource) ([]unstructured.Unstructured, error) {
	return c.list(ctx, schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource})
}

func (c *ClusterSource) list(ctx context.Context, gvr schema.GroupVersionResource) ([]unstructured.Unstructured, error) {
	for attempt := 0; ; attempt++ {
		var out []unstructured.Unstructured
		cont := ""
		var err error
		for {
			var page *unstructured.UnstructuredList
			page, err = c.dyn.Resource(gvr).List(ctx, metav1.ListOptions{Limit: pageSize, Continue: cont})
			if err != nil {
				break
			}
			out = append(out, page.Items...)
			cont = page.GetContinue()
			if cont == "" {
				return out, nil
			}
		}
		// An expired continue token restarts the listing once.
		if apierrors.IsResourceExpired(err) && attempt == 0 {
			continue
		}
		return nil, err
	}
}

// ListSecretsMeta reads Secrets through the server-side Table with metadata only: the response
// carries name, type and key count, never data.
func (c *ClusterSource) ListSecretsMeta(ctx context.Context) ([]collect.SecretMeta, error) {
	var out []collect.SecretMeta
	cont := ""
	for {
		req := c.disc.RESTClient().Get().AbsPath("/api/v1/secrets").
			Param("limit", fmt.Sprint(pageSize)).
			Param("includeObject", "Metadata").
			SetHeader("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io")
		if cont != "" {
			req = req.Param("continue", cont)
		}
		raw, err := req.Do(ctx).Raw()
		if err != nil {
			return nil, err
		}
		page, next, err := ParseSecretTable(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if next == "" {
			return out, nil
		}
		cont = next
	}
}

// ParseSecretTable decodes a meta.k8s.io/v1 Table of Secrets requested with includeObject=Metadata.
// It refuses rows whose object is not PartialObjectMetadata, so a server that ignored the
// request and sent full objects cannot leak data into the snapshot.
func ParseSecretTable(raw []byte) ([]collect.SecretMeta, string, error) {
	var tbl metav1.Table
	if err := json.Unmarshal(raw, &tbl); err != nil {
		return nil, "", fmt.Errorf("secret table: %w", err)
	}
	if tbl.Kind != "Table" {
		return nil, "", fmt.Errorf("secret table: unexpected kind %q", tbl.Kind)
	}
	typeCol, dataCol := -1, -1
	for i, col := range tbl.ColumnDefinitions {
		switch strings.ToLower(col.Name) {
		case "type":
			typeCol = i
		case "data":
			dataCol = i
		}
	}
	out := make([]collect.SecretMeta, 0, len(tbl.Rows))
	for _, row := range tbl.Rows {
		var pom metav1.PartialObjectMetadata
		if err := json.Unmarshal(row.Object.Raw, &pom); err != nil {
			return nil, "", fmt.Errorf("secret table row: %w", err)
		}
		if pom.Kind != "PartialObjectMetadata" {
			return nil, "", fmt.Errorf("secret table row: object kind %q, want PartialObjectMetadata", pom.Kind)
		}
		sm := collect.SecretMeta{Name: pom.Name, NS: pom.Namespace, Labels: pom.Labels,
			Created: pom.CreationTimestamp.Time, OwnerRefs: pom.OwnerReferences,
			ManagedFields: pom.ManagedFields, UID: string(pom.UID)}
		if typeCol >= 0 && typeCol < len(row.Cells) {
			sm.Type, _ = row.Cells[typeCol].(string)
		}
		if dataCol >= 0 && dataCol < len(row.Cells) {
			if n, ok := row.Cells[dataCol].(float64); ok {
				sm.Keys = int(n)
			}
		}
		out = append(out, sm)
	}
	return out, tbl.Continue, nil
}
