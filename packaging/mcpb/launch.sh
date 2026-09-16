#!/bin/sh
# Starts the arno-mcp binary built for this machine. The bundle carries one
# binary per supported OS and architecture; MCPB can tell operating systems
# apart but not architectures, so the choice is made here.
set -e

dir=$(cd "$(dirname "$0")" && pwd)

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "arno-mcp: $(uname -s) is not supported; see https://github.com/julianbei/arno/issues/2" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "arno-mcp: $(uname -m) is not supported" >&2; exit 1 ;;
esac

bin="$dir/arno-mcp_${os}_${arch}"
# Zip extraction does not always keep the executable bit.
[ -x "$bin" ] || chmod +x "$bin"
exec "$bin" "$@"
