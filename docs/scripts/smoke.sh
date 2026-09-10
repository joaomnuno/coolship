#!/bin/sh
# Smoke test for a running documentation server. Fetches the routes the site
# depends on and checks their status, content type, and a marker in the body.
# Usage: scripts/smoke.sh [BASE_URL]   (default http://127.0.0.1:3000)
# Exits 1 on the first failure; used by the CI container test and locally.
set -eu

base=${1:-http://127.0.0.1:3000}
base=${base%/}
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
failed=0

# check NAME PATH EXPECTED_STATUS TYPE_SUBSTRING BODY_SUBSTRING [curl args...]
check() {
  name=$1 path=$2 want_status=$3 want_type=$4 want_body=$5
  shift 5
  status=$(curl -sS -o "$tmp" -w '%{http_code}' -D "$tmp.h" "$@" "$base$path") || status=000
  type=$(tr -d '\r' < "$tmp.h" | awk 'tolower($1)=="content-type:"{print tolower($2)}' | tail -n 1)
  rm -f "$tmp.h"
  if [ "$status" != "$want_status" ]; then
    echo "FAIL $name: $path -> $status, want $want_status"; failed=1; return
  fi
  case "$type" in *"$want_type"*) ;; *)
    echo "FAIL $name: $path content-type '$type', want '$want_type'"; failed=1; return ;;
  esac
  if [ -n "$want_body" ] && ! grep -q -- "$want_body" "$tmp"; then
    echo "FAIL $name: $path body lacks '$want_body'"; failed=1; return
  fi
  echo "ok   $name: $path -> $status ($(wc -c < "$tmp") bytes)"
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
check command-page      /docs/commands/deploy            200 text/html     'observe exactly that deployment'
check nested-page       /docs/guides/ci                  200 text/html     'COOLSHIP_TOKEN'
check search            '/api/search?query=deploy'       200 json          'deploy'
check llms-txt          /llms.txt                        200 text/markdown '/docs/commands/deploy'
check llms-full         /llms-full.txt                   200 text/markdown '# deploy'
check page-markdown     /docs/commands/deploy.md         200 text/markdown '# deploy'
check index-markdown    /docs/index.md                   200 text/markdown '# Overview'
check accept-markdown   /docs/commands/deploy            200 text/markdown '# deploy' -H 'Accept: text/markdown'
check robots            /robots.txt                      200 text/plain    'User-agent'
check not-found         /docs/no-such-page               404 text/html     'Page not found'

if [ "$failed" -ne 0 ]; then exit 1; fi
echo "smoke: all routes at $base answered as expected"
