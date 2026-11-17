// Package collect turns the objects of one cluster into a sanitized snapshot. It talks to the
// cluster only through Source, which keeps the projection logic testable without a cluster.
package collect

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// APIResource is one list-able resource served by the cluster, at its preferred version.
type APIResource struct {
	Group      string
	Version    string
	Resource   string
	Kind       string
	Namespaced bool
	CRD        bool
}

// SecretMeta is what kubestrata may know about a Secret: metadata, type and key count. Never data.
type SecretMeta struct {
	Name          string
	NS            string
	Type          string
	Keys          int
	Labels        map[string]string
	Created       time.Time
	OwnerRefs     []metav1.OwnerReference
	ManagedFields []metav1.ManagedFieldsEntry
	UID           string
}

// Source is the read-only view of a cluster used by the collector.
type Source interface {
	Server() string
	ServerVersion(ctx context.Context) (string, error)
	KubeSystemUID(ctx context.Context) (string, error)
	// Discover returns the list-able resources. A partial result comes with non-nil errors.
	Discover(ctx context.Context) ([]APIResource, []error)
	List(ctx context.Context, r APIResource) ([]unstructured.Unstructured, error)
	// ListSecretsMeta must never fetch Secret data (server-side Table with metadata only).
	ListSecretsMeta(ctx context.Context) ([]SecretMeta, error)
}
