#!/bin/sh
# Smoke test for a running documentation server. Fetches the routes the site
# depends on and checks their status, content type, lines in the body, and
# the headers that matter to caches.
# Usage: scripts/smoke.sh [BASE_URL]   (default http://127.0.0.1:3000)
# Exits 1 when any check failed; used by the CI container test and locally.
set -eu

base=${1:-http://127.0.0.1:3000}
base=${base%/}
tmp=$(mktemp)
trap 'rm -f "$tmp" "$tmp.h"' EXIT
failed=0

# check NAME PATH EXPECTED_STATUS TYPE_SUBSTRING BODY_PATTERNS [curl args...]
# BODY_PATTERNS holds one extended regular expression per line; each must
# match a line of the body. The response headers stay in $tmp.h for header().
check() {
  name=$1 path=$2 want_status=$3 want_type=$4 want_body=$5
  shift 5
  status=$(curl -sS -o "$tmp" -w '%{http_code}' -D "$tmp.h" "$@" "$base$path") || status=000
  type=$(tr -d '\r' < "$tmp.h" | awk 'tolower($1)=="content-type:"{print tolower($2)}' | tail -n 1)
  if [ "$status" != "$want_status" ]; then
    echo "FAIL $name: $path -> $status, want $want_status"; failed=1; return
  fi
  case "$type" in *"$want_type"*) ;; *)
    echo "FAIL $name: $path content-type '$type', want '$want_type'"; failed=1; return ;;
  esac
  saved_ifs=$IFS
  IFS='
'
  set -f
  for pattern in $want_body; do
    if ! grep -qE -- "$pattern" "$tmp"; then
      set +f; IFS=$saved_ifs
      echo "FAIL $name: $path body has no line matching '$pattern'"; failed=1; return
    fi
  done
  set +f
  IFS=$saved_ifs
  echo "ok   $name: $path -> $status ($(wc -c < "$tmp") bytes)"
}

# header NAME HEADER TOKEN: checks that a response header of the last check
# lists TOKEN as one of its comma-separated values (case-insensitively, across
# every occurrence of the header), so `Accept` does not pass on `Accept-Encoding`.
header() {
  name=$1 want_header=$2 want_value=$3
  value=$(tr -d '\r' < "$tmp.h" | awk -v h="$(printf '%s:' "$want_header" | tr '[:upper:]' '[:lower:]')" \
    'tolower($1)==h{sub(/^[^:]*:[ \t]*/, ""); print}' | paste -sd, - | tr '[:upper:]' '[:lower:]')
  if printf '%s' "$value" | tr ',' '\n' | sed 's/^[ \t]*//; s/[ \t]*$//' | grep -qx -- "$(printf '%s' "$want_value" | tr '[:upper:]' '[:lower:]')"; then
    echo "ok   $name: $want_header lists $want_value"
  else
    echo "FAIL $name: $want_header is '$value', want $want_value listed"; failed=1
  fi
}

# Wait for the server to answer at all.
i=0
until curl -sSf -o /dev/null "$base/" 2>/dev/null; do
  i=$((i + 1))
  if [ "$i" -ge 60 ]; then echo "FAIL server at $base did not answer within 60s"; exit 1; fi
  sleep 1
done

check home              /                                200 text/html     'Link once, then ship'
check docs-index        /docs                            200 text/html     'Coolship brings a Wrangler-like'
header docs-index       Vary                             Accept
check command-page      /docs/commands/deploy            200 text/html     'observe exactly that deployment'
header command-page     Vary                             Accept
check nested-page       /docs/guides/ci                  200 text/html     'COOLSHIP_TOKEN'
check search            '/api/search?query=deploy'       200 json          'deploy'
check llms-txt          /llms.txt                        200 text/markdown '/docs/commands/deploy'
check page-markdown     /docs/commands/deploy.md         200 text/markdown '^# deploy$'
check accept-markdown   /docs/commands/deploy            200 text/markdown '^# deploy$' -H 'Accept: text/markdown'
header accept-markdown  Vary                             Accept
check accept-index      /docs                            200 text/markdown '^# Overview$' -H 'Accept: text/markdown'
header accept-index     Vary                             Accept
# Pages built from <Steps>, <Callout>, and <Cards>: every block must keep its
# own line, so a heading is a whole line, a code fence stands alone, and the
# cards form a list with one item per line.
check index-markdown    /docs/index.md                   200 text/markdown '^# Overview$
^- \[Get started\]\(
^- \[Concepts\]\('
check steps-markdown    /docs/get-started.md             200 text/markdown '^# Get started$
^## Install$
^## Log in$
^```bash$
^```$
^> \*\*Private repositories and build packs\*\*$
^- \[Monorepos\]\('
check llms-full         /llms-full.txt                   200 text/markdown '^# deploy$
^## Log in$
^- \[Concepts\]\('
check poster            /showcase-poster.svg             200 image/svg     '<svg'
check robots            /robots.txt                      200 text/plain    'User-agent'
check not-found         /docs/no-such-page               404 text/html     'Page not found'

if [ "$failed" -ne 0 ]; then exit 1; fi
echo "smoke: all routes at $base answered as expected"
