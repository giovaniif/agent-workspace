#!/bin/sh
set -eu

repo=giovaniif/crew
dir=${CREW_INSTALL_DIR:-$HOME/.local/bin}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) echo "crew: unsupported CPU $(uname -m)" >&2; exit 1 ;;
esac
case "$os/$arch" in
  darwin/arm64|darwin/amd64|linux/amd64|linux/arm64) ;;
  *) echo "crew: no build for $os/$arch" >&2; exit 1 ;;
esac

tag=${CREW_VERSION:-}
if [ -z "$tag" ] && [ -n "${CREW_DOWNLOAD_BASE:-}" ]; then
  tag=snapshot
fi
if [ -z "$tag" ]; then
  tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases?per_page=50" |
    sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | grep -v -- '-alpha' | head -n 1)
fi
[ -n "$tag" ] || { echo "crew: no release found" >&2; exit 1; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
archive=crew_${os}_${arch}.tar.gz
base=${CREW_DOWNLOAD_BASE:-https://github.com/$repo/releases/download/$tag}
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
else
  got=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
fi
[ -n "$want" ] && [ "$want" = "$got" ] || { echo "crew: checksum mismatch for $archive" >&2; exit 1; }

tar -xzf "$tmp/$archive" -C "$tmp" crew nvim
mkdir -p "$dir"
chmod 755 "$tmp/crew"
"$tmp/crew" version >/dev/null || { echo "crew: the new binary does not run" >&2; exit 1; }
mv "$tmp/crew" "$dir/crew"
echo "installed crew $tag to $dir/crew"
data=${XDG_DATA_HOME:-$HOME/.local/share}
share=$data/crew
mkdir -p "$share"
rm -rf "$share/nvim"
mv "$tmp/nvim" "$share/nvim"
echo "installed the nvim plugin to $share/nvim; run crew to set up Claude, Codex and nvim"
case ":$PATH:" in *":$dir:"*) ;; *) echo "add $dir to your PATH" ;; esac
if "$dir/crew" daemon status >/dev/null 2>&1; then
  echo "a daemon from the previous version is running: run 'crew daemon stop' so the next 'crew' starts the new one"
fi
