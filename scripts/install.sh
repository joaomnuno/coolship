#!/bin/sh
# Install coolship from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/joaomnuno/coolship/main/scripts/install.sh | sh
#
# POSIX sh; needs curl or wget, tar, and sha256sum or shasum. Linux (glibc or
# musl) and macOS on amd64 or arm64. Windows is not supported: build from
# source or use WSL. Run with --help for options.
set -eu

REPO=joaomnuno/coolship
BASE_URL=${COOLSHIP_BASE_URL:-https://github.com/$REPO}
if [ -n "${COOLSHIP_BASE_URL:-}" ]; then
	API_LATEST_URL=$COOLSHIP_BASE_URL/api/releases/latest
else
	API_LATEST_URL=https://api.github.com/repos/$REPO/releases/latest
fi
SCRIPT_URL=https://raw.githubusercontent.com/$REPO/main/scripts/install.sh

version=${COOLSHIP_VERSION:-latest}
install_dir=${COOLSHIP_INSTALL_DIR:-}
dry_run=0
tmp=

usage() {
	cat <<USAGE
Install coolship from GitHub Releases.

Usage: install.sh [--version X.Y.Z] [--dir DIR] [--dry-run] [--help]
       curl -fsSL $SCRIPT_URL | sh -s -- [options]

Options:
  --version X.Y.Z   Install this release instead of the latest one. A
                    pre-release such as 0.2.0-rc.1 must be named explicitly.
  --dir DIR         Install into DIR (created if missing).
  --dry-run         Resolve the version and print what would happen, without
                    downloading or installing anything.
  -h, --help        Show this help.

Environment:
  COOLSHIP_VERSION      Same as --version (default: latest).
  COOLSHIP_INSTALL_DIR  Same as --dir (default: \$HOME/.local/bin).
  COOLSHIP_BASE_URL     For testing: serve releases from another base URL
                        that mirrors GitHub's releases/ paths.

The archive's SHA-256 is checked against the release's checksums.txt before
anything is installed. The script never runs sudo; if DIR needs root it
prints the command to run instead.
USAGE
}

log() { printf '%s\n' "$*"; }
fail() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
has() { command -v "$1" >/dev/null 2>&1; }

cleanup() {
	if [ -n "$tmp" ]; then rm -rf "$tmp"; fi
}

make_tmp() {
	if [ -z "$tmp" ]; then
		tmp=$(mktemp -d 2>/dev/null || mktemp -d -t coolship)
		trap cleanup EXIT
		trap 'exit 130' INT
		trap 'exit 143' TERM
	fi
}

# --- arguments ---------------------------------------------------------------

while [ $# -gt 0 ]; do
	case $1 in
	-h | --help)
		usage
		exit 0
		;;
	--version)
		[ $# -ge 2 ] || fail "--version needs a value"
		version=$2
		shift
		;;
	--version=*) version=${1#--version=} ;;
	--dir)
		[ $# -ge 2 ] || fail "--dir needs a value"
		install_dir=$2
		shift
		;;
	--dir=*) install_dir=${1#--dir=} ;;
	--dry-run) dry_run=1 ;;
	*) fail "unknown option '$1' (try --help)" ;;
	esac
	shift
done

if [ -z "$install_dir" ]; then
	[ -n "${HOME:-}" ] || fail "HOME is not set; pass --dir or set COOLSHIP_INSTALL_DIR"
	install_dir=$HOME/.local/bin
fi

# --- platform ----------------------------------------------------------------

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
MINGW* | MSYS* | CYGWIN* | Windows_NT) fail "Windows is not supported by this script; build from source (see README) or use WSL" ;;
*) fail "unsupported operating system: $(uname -s)" ;;
esac

case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "unsupported architecture: $(uname -m)" ;;
esac

# A shell running under Rosetta reports x86_64 on Apple silicon; prefer the
# native binary.
if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
	[ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
	arch=arm64
fi

# --- tools -------------------------------------------------------------------

if has curl; then
	fetcher=curl
elif has wget; then
	fetcher=wget
else
	fail "curl or wget is required"
fi
has tar || fail "tar is required"

if has sha256sum; then
	sha256() { sha256sum "$1" | awk '{print $1}'; }
elif has shasum; then
	sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
	fail "sha256sum or shasum is required to verify the download; refusing to install unverified binaries"
fi

# download URL FILE
download() {
	case $fetcher in
	curl) curl -fsSL --connect-timeout 15 --retry 3 -o "$2" "$1" ;;
	wget) wget -q --timeout=15 --tries=3 -O "$2" "$1" ;;
	esac
}

# redirect_target URL: print the Location of a redirect without following it,
# or nothing when the request fails or does not redirect.
redirect_target() {
	case $fetcher in
	curl) curl -sSI --connect-timeout 15 -o /dev/null -w '%{redirect_url}' "$1" 2>/dev/null || true ;;
	wget)
		wget -q --timeout=15 --max-redirect=0 --server-response --spider "$1" 2>&1 |
			sed -n 's/^ *[Ll]ocation: *//p' | tr -d '\r' | head -n 1 || true
		;;
	esac
}

# --- version -----------------------------------------------------------------

if [ "$version" = latest ] || [ -z "$version" ]; then
	# GitHub redirects releases/latest to releases/tag/<tag>. With no published
	# release it redirects to the releases index instead, so only accept a tag.
	tag=
	location=$(redirect_target "$BASE_URL/releases/latest")
	case $location in
	*/releases/tag/*) tag=${location##*/} ;;
	esac
	if [ -z "$tag" ]; then
		# Redirects can be blocked by proxies; ask the API instead.
		make_tmp
		if download "$API_LATEST_URL" "$tmp/latest.json" 2>/dev/null; then
			tag=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -n 1)
		fi
	fi
	[ -n "$tag" ] || fail "could not determine the latest release: either $REPO has no published release yet or the request failed; pass --version X.Y.Z to install a specific tag"
	version=${tag#v}
	log "Latest release: $version"
else
	version=${version#v}
fi

case $version in
*[!0-9A-Za-z.+-]* | .* | -* | *..* | *[!0-9A-Za-z]) fail "invalid version '$version' (expected X.Y.Z or X.Y.Z-pre)" ;;
[0-9]*.[0-9]*.[0-9]*) ;;
*) fail "invalid version '$version' (expected X.Y.Z or X.Y.Z-pre)" ;;
esac

asset=coolship_${version}_${os}_${arch}.tar.gz
asset_url=$BASE_URL/releases/download/v$version/$asset
sums_url=$BASE_URL/releases/download/v$version/checksums.txt

# --- install directory -------------------------------------------------------

sudo_hint() {
	if [ -f "${0:-}" ]; then
		printf 'sudo env COOLSHIP_INSTALL_DIR=%s COOLSHIP_VERSION=%s sh %s\n' "$install_dir" "$version" "$0"
	else
		printf 'curl -fsSL %s | sudo env COOLSHIP_INSTALL_DIR=%s COOLSHIP_VERSION=%s sh\n' "$SCRIPT_URL" "$install_dir" "$version"
	fi
}

dir_ok=1
if [ -d "$install_dir" ]; then
	[ -w "$install_dir" ] || dir_ok=0
elif [ "$dry_run" = 1 ]; then
	parent=$install_dir
	while [ ! -d "$parent" ]; do parent=$(dirname "$parent"); done
	[ -w "$parent" ] || dir_ok=0
elif ! mkdir -p "$install_dir" 2>/dev/null; then
	dir_ok=0
fi

if [ "$dir_ok" = 0 ]; then
	if [ "$(id -u)" = 0 ]; then
		fail "cannot write to $install_dir"
	fi
	printf 'install.sh: cannot write to %s. This script never runs sudo itself; to install there, run:\n\n    %s\n\nor pick a user-writable directory with --dir.\n' \
		"$install_dir" "$(sudo_hint)" >&2
	exit 1
fi

# --- dry run -----------------------------------------------------------------

if [ "$dry_run" = 1 ]; then
	log "Would download $asset_url"
	log "Would verify it against $sums_url"
	log "Would install $install_dir/coolship"
	exit 0
fi

# --- download and verify -----------------------------------------------------

make_tmp

log "Downloading $asset_url"
download "$asset_url" "$tmp/$asset" || fail "download failed: $asset_url (is v$version a published release with binaries for $os/$arch?)"
download "$sums_url" "$tmp/checksums.txt" || fail "download failed: $sums_url; refusing to install without a checksum"

expected=$(awk -v name="$asset" '{ f = $2; sub(/^\*/, "", f); if (f == name) { print $1; exit } }' "$tmp/checksums.txt")
[ -n "$expected" ] || fail "checksums.txt has no entry for $asset; refusing to install"
actual=$(sha256 "$tmp/$asset")
if [ "$actual" != "$expected" ]; then
	fail "checksum mismatch for $asset
  expected $expected
  actual   $actual
The download may be corrupted or tampered with; nothing was installed."
fi
log "Verified sha256 $actual"

mkdir "$tmp/extract"
tar -xzf "$tmp/$asset" -C "$tmp/extract" || fail "could not extract $asset"
[ -f "$tmp/extract/coolship" ] || fail "archive does not contain a coolship binary"

# --- install -----------------------------------------------------------------

# Copy into the target directory first so the final rename is atomic on the
# same filesystem; a concurrent invocation never sees a half-written binary.
staged=$install_dir/.coolship.$$.tmp
cp "$tmp/extract/coolship" "$staged" || fail "could not write to $install_dir"
chmod 0755 "$staged"
mv -f "$staged" "$install_dir/coolship"

if ! installed_version=$("$install_dir/coolship" --version 2>&1); then
	printf 'install.sh: %s/coolship was installed but does not run:\n%s\n' "$install_dir" "$installed_version" >&2
	exit 1
fi
log "Installed $install_dir/coolship ($installed_version)"

# --- PATH --------------------------------------------------------------------

case ":$PATH:" in
*":$install_dir:"*) ;;
*)
	shown=$install_dir
	case $install_dir in
	"${HOME:-/nonexistent}"/*) shown="\$HOME${install_dir#"$HOME"}" ;;
	esac
	case $(basename "${SHELL:-sh}") in
	fish) hint="fish_add_path $shown" ;;
	zsh) hint="echo 'export PATH=\"$shown:\$PATH\"' >> ~/.zshrc && exec zsh" ;;
	bash)
		# The tilde is printed for the user's shell to expand, not ours.
		# shellcheck disable=SC2088
		rc='~/.bashrc'
		# shellcheck disable=SC2088
		[ "$os" = darwin ] && rc='~/.bash_profile'
		hint="echo 'export PATH=\"$shown:\$PATH\"' >> $rc && exec bash"
		;;
	*) hint="export PATH=\"$shown:\$PATH\"" ;;
	esac
	log ""
	log "$install_dir is not on your PATH. Add it with:"
	log ""
	log "    $hint"
	;;
esac
