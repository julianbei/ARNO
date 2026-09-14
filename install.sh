#!/bin/sh
# Install jade-mcp from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/julianbei/jade/main/install.sh | sh
#
# Picks the build for this OS and CPU, verifies it against the release's
# checksums.txt and installs it without sudo: /usr/local/bin when that is
# writable, ~/.local/bin otherwise.
#
# JADE_VERSION      a release tag, e.g. v0.0.8 (default: the latest release)
# JADE_INSTALL_DIR  where to put jade-mcp
# JADE_RELEASE_URL  base URL of the releases, for mirrors and tests
set -eu

repo="julianbei/jade"
version="${JADE_VERSION:-}"
base="${JADE_RELEASE_URL:-https://github.com/$repo/releases/download}"
install_dir="${JADE_INSTALL_DIR:-}"

say() { printf 'jade: %s\n' "$*"; }
fail() {
	printf 'jade: %s\n' "$*" >&2
	exit 1
}
need() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required"; }

need curl
need tar
need uname

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
darwin | linux) ;;
*) fail "no prebuilt binary for $os; build from source: https://github.com/$repo#from-source" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "no prebuilt binary for $arch; build from source: https://github.com/$repo#from-source" ;;
esac

if [ -z "$version" ]; then
	version=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" |
		sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
	[ -n "$version" ] || fail "could not look up the latest release; set JADE_VERSION, e.g. JADE_VERSION=v0.0.8"
fi

name="jade-mcp_${version}_${os}_${arch}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "downloading $name.tar.gz"
curl -fsSL "$base/$version/$name.tar.gz" -o "$tmp/$name.tar.gz" ||
	fail "download failed: $base/$version/$name.tar.gz"
curl -fsSL "$base/$version/checksums.txt" -o "$tmp/checksums.txt" ||
	fail "download failed: $base/$version/checksums.txt"

expected=$(awk -v file="$name.tar.gz" '$2 == file { print $1 }' "$tmp/checksums.txt")
[ -n "$expected" ] || fail "$name.tar.gz is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$name.tar.gz" | awk '{ print $1 }')
else
	need shasum
	actual=$(shasum -a 256 "$tmp/$name.tar.gz" | awk '{ print $1 }')
fi
[ "$actual" = "$expected" ] || fail "checksum mismatch for $name.tar.gz; nothing was installed"

tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
[ -f "$tmp/$name" ] || fail "$name.tar.gz does not contain $name"

if [ -z "$install_dir" ]; then
	if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		install_dir=/usr/local/bin
	else
		install_dir="$HOME/.local/bin"
	fi
fi
mkdir -p "$install_dir"
mv "$tmp/$name" "$install_dir/jade-mcp"
chmod 755 "$install_dir/jade-mcp"

say "installed $("$install_dir/jade-mcp" --version 2>/dev/null || echo "$version") to $install_dir/jade-mcp"
case ":$PATH:" in
*":$install_dir:"*) ;;
*) say "$install_dir is not on PATH; add it: export PATH=\"$install_dir:\$PATH\"" ;;
esac
say "next: https://github.com/$repo#trying-jade-a-guide-for-testers"
