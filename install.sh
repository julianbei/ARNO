#!/bin/sh
# Install jade-mcp from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/julianbei/jade/main/install.sh | sh
#
# Picks the build for this OS and CPU, verifies it against the release's
# checksums.txt and installs it without sudo: /usr/local/bin when that is
# writable, ~/.local/bin otherwise. Run it again to update: a jade-mcp already
# on PATH is replaced where it is, and one already at the release is left alone.
#
# JADE_VERSION      a release tag, e.g. v0.0.8 (default: the latest release)
# JADE_INSTALL_DIR  where to put jade-mcp
# JADE_RELEASE_URL  base URL of the releases, for mirrors and tests
# JADE_SERVERS      language servers to install afterwards without a menu:
#                   go,java,scala,typescript,python,rust,ruby, or all
# JADE_SKIP_SETUP   set to skip the language-server step
# JADE_ADD_TO_PATH  1 adds the install directory to the shell profile without asking
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
mingw* | msys* | cygwin* | windows*) fail "Windows isn't supported, sorry! If you'd like it to be, please upvote https://github.com/$repo/issues/2" ;;
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

existing=""
if [ -n "$install_dir" ]; then
	if [ -x "$install_dir/jade-mcp" ]; then existing="$install_dir/jade-mcp"; fi
else
	existing=$(command -v jade-mcp 2>/dev/null || true)
	if [ -n "$existing" ]; then install_dir=$(dirname "$existing"); fi
fi
current=""
if [ -n "$existing" ]; then
	current=$("$existing" --version 2>/dev/null | awk 'NR == 1 { print $2 }')
	if [ "$current" = "$version" ]; then
		say "jade-mcp $version is already installed at $existing"
		exit 0
	fi
	[ -w "$install_dir" ] || fail "$existing is not writable; re-run with sudo, or set JADE_INSTALL_DIR to install elsewhere"
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

installed=$("$install_dir/jade-mcp" --version 2>/dev/null | awk 'NR == 1 { print $2 }')
[ -n "$installed" ] || installed="$version"
if [ -n "$current" ]; then
	say "updated jade-mcp ${current:-unknown} -> $installed in $install_dir; reconnect your MCP client (/mcp in Claude Code) to use it"
else
	say "installed jade-mcp $installed to $install_dir/jade-mcp"
fi
# Not on PATH means `jade-mcp` is "command not found" right after installing.
# At a terminal, offer to add it to the shell profile; JADE_ADD_TO_PATH=1 does
# it without asking. The line is added once, and marked.
case ":$PATH:" in
*":$install_dir:"*) ;;
*)
	case "${SHELL:-}" in
	*/zsh) profile="$HOME/.zshrc" ;;
	*/bash) if [ "$os" = darwin ]; then profile="$HOME/.bash_profile"; else profile="$HOME/.bashrc"; fi ;;
	*) profile="" ;;
	esac
	line="export PATH=\"$install_dir:\$PATH\""
	add="${JADE_ADD_TO_PATH:-}"
	if [ -z "$add" ] && [ -n "$profile" ] && [ -t 1 ] && { : </dev/tty; } 2>/dev/null; then
		printf 'jade: %s is not on PATH. Add it in %s? [Y/n] ' "$install_dir" "$profile"
		answer=n
		read -r answer </dev/tty || answer=n
		case "$answer" in "" | y | Y | yes) add=1 ;; esac
	fi
	if [ "$add" = 1 ] && [ -n "$profile" ]; then
		if ! grep -qsF "$line" "$profile"; then
			printf '\n# added by the Jade installer\n%s\n' "$line" >>"$profile"
		fi
		say "added $install_dir to PATH in $profile; open a new terminal, or run: $line"
	else
		say "$install_dir is not on PATH; add it: $line"
	fi
	;;
esac
say "in your MCP client config, use this command: $install_dir/jade-mcp"

# has_setup reports whether a release has `jade-mcp install`, added after
# v0.0.8. An older binary would take the word for a server start and wait on
# the terminal for protocol messages.
has_setup() {
	printf '%s\n' "$1" | awk -F'[v.-]' '{ exit !($2 > 0 || $3 > 0 || $4 >= 9) }'
}

# After a first install, offer the language-server menu when someone is at a
# terminal to answer it; curl | sh keeps stdin for the script, so the menu
# reads the terminal directly.
servers="${JADE_SERVERS:-}"
if [ -n "${JADE_SKIP_SETUP:-}" ]; then
	:
elif [ -n "$servers" ] && has_setup "$installed"; then
	# Chosen up front, by a person or an agent: no menu, no questions.
	if [ "$servers" = "all" ]; then
		"$install_dir/jade-mcp" install --all || say "some language servers did not install; see above"
	else
		"$install_dir/jade-mcp" install --servers "$servers" || say "some language servers did not install; see above"
	fi
elif [ -z "$current" ] && has_setup "$installed" && [ -t 1 ] && { : </dev/tty; } 2>/dev/null; then
	"$install_dir/jade-mcp" install </dev/tty || say "language server setup did not finish; run jade-mcp install any time"
elif has_setup "$installed"; then
	say "add language servers any time: jade-mcp install (menu), or jade-mcp install --list --json and --servers go,java"
else
	say "language servers: https://github.com/$repo#trying-jade-a-guide-for-testers"
fi
say "next: https://github.com/$repo#trying-jade-a-guide-for-testers"
