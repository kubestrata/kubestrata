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
# custom rings with unit suffixes: the page starts on the custom scale
"$bin" render -c "$src" -o "$out/render-rings" --rings 30m,2h,12h,1d,7d,30d >/dev/null
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
check() { # page hash [text the <body> tag must also hold]
  local url="file://$out/$1/index.html#$2" tag want=${3:-}
  tag=$("$chrome" --headless=new --disable-gpu --window-size=1600,1000 --virtual-time-budget=4000 \
    --dump-dom "$url" 2>/dev/null | grep -o '<body[^>]*>' | head -1 || true)
  total=$((total + 1))
  if [[ $tag == *'data-status="ok"'* && $tag == *"$want"* ]]; then
    echo "ok    $1 #$2  ${tag#<body }"
  else
    echo "FAIL  $1 #$2  ${tag:-no <body> in the dump}${want:+  (want $want)}"
    fail=$((fail + 1))
  fi
}

for h in "" "scale=log&axis=changed&color=manager" "scale=10d&color=team&system=1" "sel=$id&ns=$ns"; do
  check render "$h"
done
# ring labels below one day read in minutes and hours
check render "scale=30m&axis=changed" 'data-rings="30m,1h,1h30m,2h,2h30m,3h,3h30m,4h,4h30m,5h,5h30m,6h"'
check render "scale=1h&color=manager&sel=$id" 'data-rings="1h,2h,3h,4h,5h,6h,7h,8h,9h,10h,11h,12h"'
check render-rings "" 'data-rings="30m,2h,12h,1d,7d,30d"'
check render-rings "scale=custom&axis=changed" 'data-rings="30m,2h,12h,1d,7d,30d"'
check render-rings "scale=1h" 'data-rings="1h,2h,3h'
for h in "" "scale=log&axis=changed&color=manager" "system=1&color=team" "color=cat&hide=config,policy" \
  "sel=$id" "ns=$ns" "ns=$ns&q=a&sel=$id" "ns=_&scale=5d" "ns=no-such-namespace"; do
  check compare "$h"
done
check compare "scale=30m&sel=$id" 'data-rings="30m,1h,'
check compare "scale=1h&axis=changed&color=manager" 'data-rings="1h,2h,'

echo "page-check: $((total - fail))/$total pages ok ($src -> $tgt)"
[ "$fail" -eq 0 ]
