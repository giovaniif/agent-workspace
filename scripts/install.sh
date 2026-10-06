#!/bin/sh
set -eu

repo=giovaniif/agent-workspace
dir=${AGENTWS_INSTALL_DIR:-$HOME/.local/bin}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) echo "agentws: unsupported CPU $(uname -m)" >&2; exit 1 ;;
esac
case "$os/$arch" in
  darwin/arm64|darwin/amd64|linux/amd64|linux/arm64) ;;
  *) echo "agentws: no build for $os/$arch" >&2; exit 1 ;;
esac

tag=${AGENTWS_VERSION:-}
if [ -z "$tag" ] && [ -n "${AGENTWS_DOWNLOAD_BASE:-}" ]; then
  tag=snapshot
fi
if [ -z "$tag" ]; then
  tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases?per_page=1" |
    sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
fi
[ -n "$tag" ] || { echo "agentws: no release found" >&2; exit 1; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
archive=agentws_${os}_${arch}.tar.gz
base=${AGENTWS_DOWNLOAD_BASE:-https://github.com/$repo/releases/download/$tag}
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
else
  got=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
fi
[ -n "$want" ] && [ "$want" = "$got" ] || { echo "agentws: checksum mismatch for $archive" >&2; exit 1; }

tar -xzf "$tmp/$archive" -C "$tmp" agentws nvim
mkdir -p "$dir"
mv "$tmp/agentws" "$dir/agentws"
chmod 755 "$dir/agentws"
echo "installed agentws $tag to $dir/agentws"
share=${XDG_DATA_HOME:-$HOME/.local/share}/agentws
mkdir -p "$share"
rm -rf "$share/nvim"
mv "$tmp/nvim" "$share/nvim"
echo "installed the nvim plugin to $share/nvim; run agentws to set up Claude, Codex and nvim"
case ":$PATH:" in *":$dir:"*) ;; *) echo "add $dir to your PATH" ;; esac
if "$dir/agentws" daemon status >/dev/null 2>&1; then
  echo "a daemon from the previous version is running: run 'agentws daemon stop' so the next 'agentws' starts the new one"
fi
