#!/bin/sh
# Test scripts/install.sh against a local fake of GitHub Releases.
#
# Usage: scripts/install_test.sh            (runs install.sh with sh)
#        TEST_SH=dash scripts/install_test.sh
#
# Needs python3 (for the fake server), curl and wget, tar, sha256sum or shasum.
set -eu

here=$(cd -- "$(dirname -- "$0")" && pwd)
install=$here/install.sh
SH=${TEST_SH:-sh}

for tool in python3 curl wget tar; do
	command -v "$tool" >/dev/null 2>&1 || { echo "install_test.sh: $tool is required" >&2; exit 2; }
done
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
	echo "install_test.sh: sha256sum or shasum is required" >&2
	exit 2
fi

work=$(mktemp -d)
pids=
cleanup() {
	for pid in $pids; do kill "$pid" 2>/dev/null || true; done
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) echo "install_test.sh: unsupported OS" >&2; exit 2 ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) echo "install_test.sh: unsupported architecture" >&2; exit 2 ;;
esac
if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
	[ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
	arch=arm64
fi

# --- fake release site -------------------------------------------------------

site=$work/site
mkdir -p "$site/api/releases"

# make_release VERSION [tamper]: publish a stub binary that prints its version.
make_release() {
	dir=$site/releases/download/v$1
	stage=$work/stage-$1
	mkdir -p "$dir" "$stage"
	printf '#!/bin/sh\necho "coolship version v%s"\n' "$1" >"$stage/coolship"
	chmod 0755 "$stage/coolship"
	echo "# Coolship" >"$stage/README.md"
	echo "# Changelog" >"$stage/CHANGELOG.md"
	asset=coolship_${1}_${os}_${arch}.tar.gz
	tar -czf "$dir/$asset" -C "$stage" coolship README.md CHANGELOG.md
	sum=$(sha256 "$dir/$asset")
	if [ "${2:-}" = tamper ]; then
		sum=0000000000000000000000000000000000000000000000000000000000000000
	fi
	printf '%s  %s\n' "$sum" "$asset" >"$dir/checksums.txt"
	# GoReleaser lists every artifact; add a foreign one to prove matching by name.
	printf '%s  coolship_%s_windows_amd64.zip\n' "$sum" "$1" >>"$dir/checksums.txt"
}

make_release 0.8.0
make_release 0.9.0
make_release 1.0.0-rc.1
make_release 0.7.0 tamper
echo v0.9.0 >"$site/latest"
printf '{"tag_name": "v0.9.0", "name": "Coolship v0.9.0"}\n' >"$site/api/releases/latest"

cat >"$work/server.py" <<'PY'
import http.server, os, sys

root = sys.argv[1]
# Modes: "" (GitHub with a release), "no-redirect" (redirects blocked, API
# works), "no-release" (GitHub with only tags: redirect to the index, API 404).
mode = sys.argv[2] if len(sys.argv) > 2 else ""


class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=root, **kwargs)

    def redirect(self, location):
        self.send_response(302)
        self.send_header("Location", location)
        self.end_headers()

    def latest(self):
        if self.path == "/api/releases/latest" and mode == "no-release":
            self.send_error(404)
            return True
        if self.path != "/releases/latest":
            return False
        if mode == "no-redirect":
            self.send_error(404)
        elif mode == "no-release":
            self.redirect("/releases")
        else:
            with open(os.path.join(root, "latest")) as f:
                self.redirect("/releases/tag/" + f.read().strip())
        return True

    def do_HEAD(self):
        if not self.latest():
            super().do_HEAD()

    def do_GET(self):
        if not self.latest():
            super().do_GET()

    def log_message(self, *args):
        pass


server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
print(server.server_address[1], flush=True)
server.serve_forever()
PY

# start_server NAME [no-redirect]: sets $base_url. Not run in a subshell so
# the trap can stop the server.
start_server() {
	python3 "$work/server.py" "$site" "${2:-}" >"$work/$1.port" 2>/dev/null &
	pids="$pids $!"
	i=0
	while [ ! -s "$work/$1.port" ]; do
		i=$((i + 1))
		[ $i -lt 100 ] || { echo "install_test.sh: server did not start" >&2; exit 2; }
		sleep 0.1
	done
	base_url="http://127.0.0.1:$(cat "$work/$1.port")"
}

start_server main
base=$base_url
start_server noredirect no-redirect
base_noredirect=$base_url
start_server norelease no-release
base_norelease=$base_url

# --- tool shims --------------------------------------------------------------

# make_shim DIR [excluded...]: a PATH with only the tools install.sh needs.
make_shim() {
	shim=$1
	shift
	mkdir -p "$shim"
	for tool in sh dash bash uname mktemp rm mkdir cp mv chmod tar gzip awk sed grep tr head basename dirname id cat sysctl sleep curl wget sha256sum shasum; do
		case " $* " in *" $tool "*) continue ;; esac
		path=$(command -v "$tool" 2>/dev/null) || continue
		ln -s "$path" "$shim/$tool"
	done
}
make_shim "$work/bin-all"
make_shim "$work/bin-wget" curl
make_shim "$work/bin-nofetch" curl wget
make_shim "$work/bin-nosha" sha256sum shasum

# --- harness -----------------------------------------------------------------

passed=0
failed=0
n=0
home=$work/home
mkdir -p "$home"

# run SHIM BASE_URL [env...] -- [args...]: runs install.sh, records status.
run() {
	n=$((n + 1))
	shim=$1
	url=$2
	shift 2
	set -- PATH="$shim" HOME="$home" TMPDIR="$work" SHELL=/bin/bash COOLSHIP_BASE_URL="$url" "$@"
	status=0
	env -i "$@" >"$work/out.$n" 2>"$work/err.$n" || status=$?
	out=$work/out.$n
	err=$work/err.$n
}

ok() { passed=$((passed + 1)); }
ko() {
	failed=$((failed + 1))
	printf 'FAIL: %s\n' "$1" >&2
	[ -f "${out:-}" ] && sed 's/^/  out: /' "$out" >&2
	[ -f "${err:-}" ] && sed 's/^/  err: /' "$err" >&2
}
assert_status() { if [ "$status" = "$1" ]; then ok; else ko "$2 (status $status, want $1)"; fi; }
assert_contains() { if grep -qF -- "$2" "$1"; then ok; else ko "$3 (missing '$2')"; fi; }
assert_file() { if [ -x "$1" ]; then ok; else ko "$2 (no executable at $1)"; fi; }
assert_no_file() { if [ ! -e "$1" ]; then ok; else ko "$2 (unexpected $1)"; fi; }

# --- tests -------------------------------------------------------------------

echo "== latest via redirect (curl), default dir, bash PATH hint"
run "$work/bin-all" "$base" "$SH" "$install"
assert_status 0 "latest install"
assert_file "$home/.local/bin/coolship" "latest install"
assert_contains "$out" "Latest release: 0.9.0" "latest resolves the redirect"
assert_contains "$out" "coolship version v0.9.0" "latest prints the installed version"
assert_contains "$out" "Verified sha256" "latest verifies the checksum"
assert_contains "$out" "\$HOME/.local/bin" "PATH hint substitutes HOME"
assert_contains "$out" ">> ~/.bashrc" "PATH hint for bash"
if [ "$(find "$home/.local/bin" -mindepth 1 | wc -l)" = 1 ]; then ok; else ko "staging file left behind in $home/.local/bin"; fi

echo "== upgrade over an existing install"
run "$work/bin-all" "$base" COOLSHIP_VERSION=0.8.0 "$SH" "$install"
assert_status 0 "install 0.8.0 over 0.9.0"
assert_contains "$out" "coolship version v0.8.0" "0.8.0 replaces the previous binary"
run "$work/bin-all" "$base" "$SH" "$install"
assert_contains "$out" "coolship version v0.9.0" "latest replaces 0.8.0"

echo "== explicit version by flag, --dir, fish hint"
run "$work/bin-all" "$base" SHELL=/usr/bin/fish "$SH" "$install" --version v0.8.0 --dir "$work/flagdir"
assert_status 0 "--version --dir"
assert_file "$work/flagdir/coolship" "--dir creates the directory"
assert_contains "$out" "coolship version v0.8.0" "--version strips the leading v"
assert_contains "$out" "fish_add_path $work/flagdir" "PATH hint for fish"

echo "== pre-release by env"
run "$work/bin-all" "$base" COOLSHIP_VERSION=1.0.0-rc.1 COOLSHIP_INSTALL_DIR="$work/pre" "$SH" "$install"
assert_status 0 "pre-release install"
assert_contains "$out" "coolship version v1.0.0-rc.1" "pre-release installs"

echo "== latest via wget when curl is absent"
run "$work/bin-wget" "$base" COOLSHIP_INSTALL_DIR="$work/wget" "$SH" "$install"
assert_status 0 "wget install"
assert_contains "$out" "coolship version v0.9.0" "wget resolves and installs latest"

echo "== latest via API when the redirect is unavailable"
run "$work/bin-all" "$base_noredirect" COOLSHIP_INSTALL_DIR="$work/api" "$SH" "$install"
assert_status 0 "API fallback (curl)"
assert_contains "$out" "coolship version v0.9.0" "API fallback resolves latest"
run "$work/bin-wget" "$base_noredirect" COOLSHIP_INSTALL_DIR="$work/api-wget" "$SH" "$install"
assert_status 0 "API fallback (wget)"

echo "== no published release yet"
run "$work/bin-all" "$base_norelease" COOLSHIP_INSTALL_DIR="$work/norelease" "$SH" "$install"
assert_status 1 "no release (curl)"
assert_contains "$err" "no published release yet" "index redirect is not taken for a tag"
assert_no_file "$work/norelease/coolship" "no release installs nothing"
run "$work/bin-wget" "$base_norelease" COOLSHIP_INSTALL_DIR="$work/norelease" "$SH" "$install"
assert_status 1 "no release (wget)"
run "$work/bin-all" "$base_norelease" COOLSHIP_VERSION=0.8.0 COOLSHIP_INSTALL_DIR="$work/norelease" "$SH" "$install"
assert_status 0 "explicit version still works without a latest release"
assert_contains "$out" "coolship version v0.8.0" "explicit version installs from tag URL"

echo "== tampered checksum is refused"
run "$work/bin-all" "$base" COOLSHIP_VERSION=0.7.0 COOLSHIP_INSTALL_DIR="$work/tampered" "$SH" "$install"
assert_status 1 "tampered checksum"
assert_contains "$err" "checksum mismatch" "tampered checksum is reported"
assert_no_file "$work/tampered/coolship" "tampered archive is not installed"

echo "== unpublished version"
run "$work/bin-all" "$base" COOLSHIP_VERSION=0.0.1 COOLSHIP_INSTALL_DIR="$work/missing" "$SH" "$install"
assert_status 1 "unpublished version"
assert_contains "$err" "download failed" "unpublished version is reported"
assert_no_file "$work/missing/coolship" "unpublished version installs nothing"

echo "== invalid version"
run "$work/bin-all" "$base" COOLSHIP_INSTALL_DIR="$work/invalid" "$SH" "$install" --version '0.1;rm'
assert_status 1 "invalid version"
assert_contains "$err" "invalid version" "invalid version is rejected"

echo "== missing tools"
run "$work/bin-nofetch" "$base" COOLSHIP_INSTALL_DIR="$work/nofetch" "$SH" "$install"
assert_status 1 "no curl or wget"
assert_contains "$err" "curl or wget is required" "missing fetcher is reported"
run "$work/bin-nosha" "$base" COOLSHIP_INSTALL_DIR="$work/nosha" "$SH" "$install"
assert_status 1 "no sha256sum or shasum"
assert_contains "$err" "sha256sum or shasum is required" "missing hasher is reported"
assert_no_file "$work/nosha/coolship" "nothing installed without a hasher"

echo "== dry run"
run "$work/bin-all" "$base" COOLSHIP_INSTALL_DIR="$work/dry" "$SH" "$install" --dry-run
assert_status 0 "dry run"
assert_contains "$out" "Would download $base/releases/download/v0.9.0/coolship_0.9.0_${os}_${arch}.tar.gz" "dry run prints the URL"
assert_no_file "$work/dry" "dry run creates nothing"

echo "== help"
run "$work/bin-all" "$base" "$SH" "$install" --help
assert_status 0 "help"
assert_contains "$out" "COOLSHIP_BASE_URL" "help documents the overrides"
run "$work/bin-all" "$base" "$SH" "$install" --bogus
assert_status 1 "unknown option"
assert_contains "$err" "unknown option" "unknown option is reported"

if [ "$(id -u)" != 0 ]; then
	echo "== unwritable directory prints the sudo command"
	mkdir -p "$work/ro"
	chmod 0555 "$work/ro"
	run "$work/bin-all" "$base" "$SH" "$install" --dir "$work/ro/bin"
	assert_status 1 "unwritable dir"
	assert_contains "$err" "sudo env COOLSHIP_INSTALL_DIR=$work/ro/bin COOLSHIP_VERSION=0.9.0 sh $install" "sudo hint names the exact command"
	assert_no_file "$work/ro/bin" "unwritable dir installs nothing"
	chmod 0755 "$work/ro"
else
	echo "== skipping unwritable directory test as root"
fi

echo
echo "$passed passed, $failed failed"
[ "$failed" = 0 ]
