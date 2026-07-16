#!/usr/bin/env sh
# hostrunner plugin launcher (macOS/Linux).
# Downloads the prebuilt hostrunner binary matching this host's OS/arch from
# GitHub Releases, verifies it against the release checksums.txt, caches it,
# then execs it. Windows is not supported by this launcher — install the
# binary manually and configure the MCP server via claude_desktop_config.json
# (see README).
set -eu

REPO="jerryctt/hostrunner-mcp"
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
PLUGIN_JSON="$PLUGIN_ROOT/.claude-plugin/plugin.json"

VER="$(grep '"version"' "$PLUGIN_JSON" | head -1 | sed -E 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
if [ -z "${VER:-}" ]; then
  echo "hostrunner launcher: cannot read version from $PLUGIN_JSON" >&2
  exit 1
fi
TAG="v$VER"

os="$(uname -s)"; arch="$(uname -m)"
case "$os" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) echo "hostrunner launcher: unsupported OS '$os' — install manually (see README)" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "hostrunner launcher: unsupported arch '$arch'" >&2; exit 1 ;;
esac

# Cache outside the plugin dir: claude.ai-hosted plugins reject top-level
# bin/ content, and the managed install dir may not be writable.
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/hostrunner"
BIN="$CACHE_DIR/hostrunner-$VER-$os-$arch"

if [ ! -x "$BIN" ]; then
  mkdir -p "$CACHE_DIR"
  archive="hostrunner_${VER}_${os}_${arch}.tar.gz"
  base="https://github.com/$REPO/releases/download/$TAG"
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  echo "hostrunner launcher: downloading $archive ($TAG)..." >&2
  curl -fsSL -o "$tmp/$archive" "$base/$archive"
  curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

  want="$(grep " ${archive}\$" "$tmp/checksums.txt" | awk '{print $1}')"
  if [ -z "${want:-}" ]; then
    echo "hostrunner launcher: $archive not listed in checksums.txt" >&2; exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    got="$(sha256sum "$tmp/$archive" | awk '{print $1}')"
  else
    got="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
  fi
  if [ "$want" != "$got" ]; then
    echo "hostrunner launcher: checksum mismatch for $archive" >&2
    echo "  expected $want" >&2
    echo "  got      $got" >&2
    exit 1
  fi

  tar -xzf "$tmp/$archive" -C "$tmp"
  if [ ! -f "$tmp/hostrunner" ]; then
    echo "hostrunner launcher: 'hostrunner' not found in archive" >&2; exit 1
  fi
  chmod +x "$tmp/hostrunner"
  mv "$tmp/hostrunner" "$BIN"
  rm -rf "$tmp"; trap - EXIT
  echo "hostrunner launcher: installed $BIN" >&2
fi

exec "$BIN" "$@"
