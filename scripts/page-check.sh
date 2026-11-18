#!/usr/bin/env bash
# page-check: writes the render and compare sites from the cache (no cluster access) into a temp
# dir, loads each index.html in headless Chrome for several URL-hash states and fails unless every
# page reports data-status="ok" on <body>.
#
#   KUBESTRATA_CACHE_DIR  cache to read (default: the kubestrata user cache)
#   PAGE_CHECK_SOURCE     source context (default: kuberito-source)
#   PAGE_CHECK_TARGET     target context (default: kuberito-target)
#   KUBESTRATA_BIN        binary (default: bin/kubestrata)
#   CHROME                browser binary (default: Google Chrome on macOS, else google-chrome or chromium)
#
# Run `kubestrata collect -c <source> -c <target>` first.
set -euo pipefail

src=${PAGE_CHECK_SOURCE:-kuberito-source}
tgt=${PAGE_CHECK_TARGET:-kuberito-target}
bin=${KUBESTRATA_BIN:-bin/kubestrata}

chrome=${CHROME:-}
if [ -z "$chrome" ]; then
  mac="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
  if [ -x "$mac" ]; then
    chrome=$mac
  else
    for c in google-chrome chromium; do
      if command -v "$c" >/dev/null 2>&1; then chrome=$c; break; fi
    done
  fi
fi
if [ -z "$chrome" ]; then
  echo "page-check: Google Chrome or Chromium not found; set CHROME" >&2
  exit 1
fi

out=$(mktemp -d "${TMPDIR:-/tmp}/kubestrata-page-check.XXXXXX")
trap 'rm -rf "$out"' EXIT

"$bin" render -c "$src" -o "$out/render" >/dev/null
"$bin" compare -c "$src" -c "$tgt" -o "$out/compare" --format json >/dev/null

# A real drift pair (or the first item) and its namespace, for the selection and filter states.
read -r id ns < <(awk '
  /"items": \[/ { items = 1 }
  !items { next }
  /"id": / { id = $0; sub(/^[^:]*: "/, "", id); sub(/",?$/, "", id) }
  /"ns": / { ns = $0; sub(/^[^:]*: "/, "", ns); sub(/",?$/, "", ns); if (first == "") first = id " " ns }
  /"state": "drift"/ { print id " " ns; found = 1; exit }
  END { if (!found && first != "") print first }
' "$out/compare/json/compare.json") || true
if [ -z "${id:-}" ]; then
  echo "page-check: the comparison of $src and $tgt has no items" >&2
  exit 1
fi

fail=0 total=0
check() { # page hash
  local url="file://$out/$1/index.html#$2" tag
  tag=$("$chrome" --headless=new --disable-gpu --window-size=1600,1000 --virtual-time-budget=4000 \
    --dump-dom "$url" 2>/dev/null | grep -o '<body[^>]*>' | head -1 || true)
  total=$((total + 1))
  if [[ $tag == *'data-status="ok"'* ]]; then
    echo "ok    $1 #$2  ${tag#<body }"
  else
    echo "FAIL  $1 #$2  ${tag:-no <body> in the dump}"
    fail=$((fail + 1))
  fi
}

for h in "" "scale=log&axis=changed&color=manager" "scale=10d&color=team&system=1" "sel=$id&ns=$ns"; do
  check render "$h"
done
for h in "" "scale=log&axis=changed&color=manager" "system=1&color=team" "color=cat&hide=config,policy" \
  "sel=$id" "ns=$ns" "ns=$ns&q=a&sel=$id" "ns=_&scale=5d" "ns=no-such-namespace"; do
  check compare "$h"
done

echo "page-check: $((total - fail))/$total pages ok ($src -> $tgt)"
[ "$fail" -eq 0 ]
