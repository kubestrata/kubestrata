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
# rings in years for long-lived clusters, and the auto scale (quantiles of the resource ages)
"$bin" render -c "$src" -o "$out/render-years" --rings 30d,90d,180d,1y,2y,3y,5y >/dev/null
"$bin" render -c "$src" -o "$out/render-auto" --scale auto >/dev/null
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
body() { # page hash → the <body> tag after the first draw
  "$chrome" --headless=new --disable-gpu --window-size=1600,1000 --virtual-time-budget=4000 \
    --dump-dom "file://$out/$1/index.html#$2" 2>/dev/null | grep -o '<body[^>]*>' | head -1 || true
}
attr() { # tag name → value of the attribute
  local re="$2=\"([^\"]*)\""
  if [[ $1 =~ $re ]]; then printf '%s' "${BASH_REMATCH[1]}"; fi
}
check() { # page hash [text the <body> tag must also hold] [text it must not hold]
  local tag want=${3:-} deny=${4:-}
  tag=$(body "$1" "$2")
  total=$((total + 1))
  if [[ $tag == *'data-status="ok"'* && $tag == *"$want"* && ( -z $deny || $tag != *"$deny"* ) ]]; then
    echo "ok    $1 #$2  ${tag#<body }"
  else
    echo "FAIL  $1 #$2  ${tag:-no <body> in the dump}${want:+  (want $want)}${deny:+  (not $deny)}"
    fail=$((fail + 1))
  fi
}

for h in "" "scale=log&axis=changed&color=manager" "scale=10d&color=team&system=1" "sel=$id&ns=$ns" "show=platform&hide=config"; do
  check render "$h"
done
# ring labels below one day read in minutes and hours
check render "scale=30m&axis=changed" 'data-rings="30m,1h,1h30m,2h,2h30m,3h,3h30m,4h,4h30m,5h,5h30m,6h"'
check render "scale=1h&color=manager&sel=$id" 'data-rings="1h,2h,3h,4h,5h,6h,7h,8h,9h,10h,11h,12h"'
check render-rings "" 'data-rings="30m,2h,12h,1d,7d,30d"'
check render-rings "scale=custom&axis=changed" 'data-rings="30m,2h,12h,1d,7d,30d"'
check render-rings "scale=1h" 'data-rings="1h,2h,3h'
# long-lived clusters: the years preset, rings in years, and auto (rings depend on the lab's ages)
check render "scale=years" 'data-rings="30d,90d,180d,1y,2y,3y,4y,5y,7y,10y"'
check render-years "" 'data-rings="30d,90d,180d,1y,2y,3y,5y"'
check render-auto "" 'data-rings="' 'data-rings="30d,60d,90d'
check render "scale=auto&axis=changed&color=manager" 'data-rings="' 'data-rings="30d,60d,90d'
for h in "" "scale=log&axis=changed&color=manager" "system=1&color=team" "color=cat&hide=config,policy" \
  "sel=$id" "ns=$ns" "ns=$ns&q=a&sel=$id" "ns=_&scale=5d" "ns=no-such-namespace"; do
  check compare "$h"
done
check compare "scale=30m&sel=$id" 'data-rings="30m,1h,'
check compare "scale=1h&axis=changed&color=manager" 'data-rings="1h,2h,'
check compare "scale=years&color=team" 'data-rings="30d,90d,180d,1y,'
check compare "scale=auto&sel=$id" 'data-rings="' 'data-rings="30d,60d,90d'
check compare "q=ns:$ns%20-kind:Service&color=team" 'data-match="'
# a kind translation (Route→HTTPRoute, DeploymentConfig→Deployment), selected by its target id
tid=$(awk '/"target_id": / { v = $0; sub(/^[^:]*: "/, "", v); sub(/",?$/, "", v); print v; exit }' "$out/compare/json/compare.json")
if [ -n "$tid" ]; then
  check compare "sel=$tid&color=state"
  check compare "q=kind:Route,kind:HTTPRoute&sel=$tid" 'data-match="'
else
  echo "      no kind translation in the comparison of $src and $tgt: selection state skipped"
fi

# search: several terms (OR), exclusion and a field prefix, checked by the number of matching
# resources the render page reports in data-match against data-shown
assert() { # description condition
  total=$((total + 1))
  if eval "$2"; then echo "ok    search: $1"; else echo "FAIL  search: $1"; fail=$((fail + 1)); fi
}
matches() { attr "$(body render "$1")" data-match; }
shown=$(attr "$(body render "")" data-shown)
m_ns=$(matches "q=$ns"); m_other=$(matches "q=-$ns"); m_or=$(matches "q=$ns,no-such-term-$RANDOM")
m_dep=$(matches "q=kind:Deployment"); m_svc=$(matches "q=kind:Service"); m_both=$(matches "q=kind:Deployment,kind:Service")
m_field=$(matches "q=ns:$ns"); m_and=$(matches "q=ns:$ns%20kind:Deployment")
echo "      search counts: shown $shown, '$ns' $m_ns, '-$ns' $m_other, '$ns,x' $m_or, kind:Deployment $m_dep," \
  "kind:Service $m_svc, both $m_both, ns:$ns $m_field, ns:$ns kind:Deployment $m_and"
assert "one term matches ($ns: $m_ns)" '[ "${m_ns:-0}" -gt 0 ]'
assert "OR: any term matches ($ns,x = $m_or; kind:Deployment,kind:Service $m_both = $m_dep + $m_svc)" \
  '[ "$m_or" = "$m_ns" ] && [ "${m_svc:-0}" -gt 0 ] && [ "$m_both" = "$((m_dep + m_svc))" ]'
assert "exclusion is the complement (-$ns: $m_other = $shown - $m_ns)" '[ "$m_other" = "$((shown - m_ns))" ]'
assert "a field prefix narrows, AND across fields (ns:$ns kind:Deployment $m_and <= ns:$ns $m_field, kind:Deployment $m_dep)" \
  '[ "${m_and:-0}" -gt 0 ] && [ "$m_and" -le "$m_field" ] && [ "$m_and" -le "$m_dep" ] && [ "$m_field" -le "$m_ns" ]'

echo "page-check: $((total - fail))/$total pages ok ($src -> $tgt)"
[ "$fail" -eq 0 ]
