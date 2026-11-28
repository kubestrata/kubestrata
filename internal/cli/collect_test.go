package cli

import (
	"strings"
	"testing"
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
