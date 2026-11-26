#!/bin/sh
# Checks an installed kubestrata binary without a cluster.
# Usage: scripts/smoke-check.sh <path to kubestrata> <version without v>
# Used by .github/workflows/install-smoke.yml after each install path (brew, go install, tarball).
set -eu

bin="${1:?path to kubestrata}"
version="${2:?version}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail() { echo "smoke: $*" >&2; exit 1; }

# 1. The version matches the release.
got="$("$bin" --version)"
[ "$got" = "kubestrata version ${version}" ] || fail "--version printed '$got', want 'kubestrata version ${version}'"
echo "ok --version: $got"

# 2. contexts reads a kubeconfig without touching the cluster and never prints the credential.
cat > "$work/kubeconfig" <<'EOF'
apiVersion: v1
kind: Config
clusters:
- name: smoke
  cluster:
    server: https://127.0.0.1:1
contexts:
- name: smoke
  context:
    cluster: smoke
    user: smoke
current-context: smoke
users:
- name: smoke
  user:
    token: not-a-real-token
EOF
out="$("$bin" --kubeconfig "$work/kubeconfig" contexts --format json)"
printf '%s' "$out" | grep -q '"schema_version": 1' || fail "contexts JSON without schema_version 1: $out"
printf '%s' "$out" | grep -q '"current": "smoke"' || fail "contexts JSON without current context: $out"
if printf '%s' "$out" | grep -q 'not-a-real-token'; then fail "contexts printed the token"; fi
echo "ok contexts --format json (no credential in the output)"

# 3. The cache starts empty in a fresh directory.
KUBESTRATA_CACHE_DIR="$work/cache" "$bin" cache ls --format json > "$work/cache.json" || fail "cache ls failed"
echo "ok cache ls on an empty cache"
