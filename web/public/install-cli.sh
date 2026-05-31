#!/usr/bin/env sh
# install-cli.sh — one-line installer for the customer-facing `iogrid` CLI.
#
# Usage:
#   curl -fsSL https://iogrid.org/install-cli.sh | sh
#
# Downloads the latest release binary for the user's OS+arch from
# https://github.com/iogrid/iogrid/releases/latest and drops it in
# /usr/local/bin (or ~/.local/bin if no sudo). After install:
#
#   iogrid login --api-key=KEY --customer-id=ID
#   iogrid vpn connect --region us-east-1
#
# Closes #527. Distinct from installer/install.sh which installs the
# provider-side `iogridd` daemon — that's the residential machine
# making bandwidth available; this is the customer using bandwidth.
#
# Env knobs:
#   IOGRID_VERSION   pin to a tag (e.g. v0.2.0); default "latest"
#   IOGRID_PREFIX    install dir override; default /usr/local/bin or ~/.local/bin
#   IOGRID_GITHUB    release host override (offline / mirror); default github.com

set -eu

IOGRID_VERSION="${IOGRID_VERSION:-latest}"
IOGRID_GITHUB="${IOGRID_GITHUB:-https://github.com}"
RELEASE_REPO="iogrid/iogrid"

# ---- OS + arch detection --------------------------------------------------

uname_s="$(uname -s)"
uname_m="$(uname -m)"

case "$uname_s" in
  Linux)   OS="linux" ;;
  Darwin)  OS="darwin" ;;
  MINGW*|MSYS*|CYGWIN*)
    echo "Detected Windows-like shell. Use the .msi installer from releases instead." >&2
    exit 1
    ;;
  *)
    echo "Unsupported OS: $uname_s" >&2
    exit 1
    ;;
esac

case "$uname_m" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Unsupported arch: $uname_m" >&2
    exit 1
    ;;
esac

# ---- install dir (sudo-free if possible) ---------------------------------

if [ -n "${IOGRID_PREFIX:-}" ]; then
  PREFIX="$IOGRID_PREFIX"
elif [ -w /usr/local/bin ] 2>/dev/null || [ "$(id -u)" = "0" ]; then
  PREFIX="/usr/local/bin"
else
  PREFIX="${HOME}/.local/bin"
  mkdir -p "$PREFIX"
fi

# ---- download URL --------------------------------------------------------

BIN="iogrid-${OS}-${ARCH}"
if [ "$IOGRID_VERSION" = "latest" ]; then
  URL="${IOGRID_GITHUB}/${RELEASE_REPO}/releases/latest/download/${BIN}"
else
  URL="${IOGRID_GITHUB}/${RELEASE_REPO}/releases/download/${IOGRID_VERSION}/${BIN}"
fi

echo "Installing iogrid CLI"
echo "  os/arch: ${OS}/${ARCH}"
echo "  source : ${URL}"
echo "  target : ${PREFIX}/iogrid"

# ---- fetch + install -----------------------------------------------------

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

if ! curl -fsSL "$URL" -o "$tmp"; then
  echo "ERROR: download failed (URL: $URL)" >&2
  echo "      check that release ${IOGRID_VERSION} has an asset named ${BIN}" >&2
  exit 1
fi

chmod +x "$tmp"

if [ -w "$PREFIX" ] || [ "$(id -u)" = "0" ]; then
  mv "$tmp" "${PREFIX}/iogrid"
else
  echo "Need sudo to write ${PREFIX}/iogrid …"
  sudo mv "$tmp" "${PREFIX}/iogrid"
fi
trap - EXIT

# ---- verify --------------------------------------------------------------

if ! command -v iogrid >/dev/null 2>&1; then
  echo
  echo "Installed to ${PREFIX}/iogrid — but it's not in your PATH."
  echo "Add this to your shell profile:"
  echo "  export PATH=\"${PREFIX}:\$PATH\""
else
  echo
  echo "✓ Installed."
  iogrid version
fi

echo
echo "Next steps:"
echo "  1. Get an API key + customer ID at https://iogrid.org/customer/vpn"
echo "  2. iogrid login --api-key=YOUR_KEY --customer-id=YOUR_ID"
echo "  3. iogrid vpn connect --region us-east-1"
echo "  4. curl ifconfig.me      # verify your exit IP changed"
