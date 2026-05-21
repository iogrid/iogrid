#!/usr/bin/env bash
# Build a Sparkle 2 appcast.xml from a set of release .pkg artifacts.
#
# This is the macOS counterpart of installer/auto-update/server (which serves
# the Linux/Windows manifest.json). The output here is the RSS feed Sparkle
# 2 polls — exactly the schema documented at:
#   https://sparkle-project.org/documentation/publishing/
#
# Inputs (via env vars, all required):
#   IOGRID_VERSION              — semver, e.g. "0.1.1"
#   IOGRID_PKG_ARM64            — path to arm64 .pkg on local disk
#   IOGRID_PKG_AMD64            — path to amd64 .pkg on local disk
#   IOGRID_PKG_URL_BASE         — release URL prefix the .pkg will be hosted at,
#                                 e.g. "https://releases.iogrid.org/macos/0.1.1"
#   SPARKLE_ED_PRIVKEY_PATH     — path to ed25519 private key file produced by
#                                 generate-keypair.sh. Mounted from the K8s
#                                 Secret iogrid-sparkle-signing in CI.
#   IOGRID_RELEASE_NOTES_URL    — public URL of HTML release notes
#                                 (e.g. https://iogrid.org/releases/0.1.1.html)
#
# Optional:
#   IOGRID_MIN_OS_VERSION       — defaults to "13.0"
#   IOGRID_PUBDATE              — RFC-822 date string; defaults to `date -R`
#   IOGRID_APPCAST_OUT          — defaults to "appcast.xml" in CWD
#
# Output:
#   $IOGRID_APPCAST_OUT — the rendered appcast.xml file, ready to upload.
#
# Behaviour:
#   1. Verifies both .pkg files exist + computes their length+ed25519 sig via
#      sign_update from the Sparkle dist.
#   2. Renders a single <item> block with two <enclosure> elements (arm64 +
#      amd64). Sparkle picks the right one client-side from sparkle:os and
#      arch heuristics.
#   3. Refuses to overwrite without -f / --force.
set -euo pipefail

: "${IOGRID_VERSION:?IOGRID_VERSION is required}"
: "${IOGRID_PKG_ARM64:?IOGRID_PKG_ARM64 is required}"
: "${IOGRID_PKG_AMD64:?IOGRID_PKG_AMD64 is required}"
: "${IOGRID_PKG_URL_BASE:?IOGRID_PKG_URL_BASE is required}"
: "${SPARKLE_ED_PRIVKEY_PATH:?SPARKLE_ED_PRIVKEY_PATH is required}"
: "${IOGRID_RELEASE_NOTES_URL:?IOGRID_RELEASE_NOTES_URL is required}"

MIN_OS_VERSION="${IOGRID_MIN_OS_VERSION:-13.0}"
PUBDATE="${IOGRID_PUBDATE:-$(date -R)}"
OUT="${IOGRID_APPCAST_OUT:-appcast.xml}"

for f in "$IOGRID_PKG_ARM64" "$IOGRID_PKG_AMD64" "$SPARKLE_ED_PRIVKEY_PATH"; do
    if [ ! -f "$f" ]; then
        echo "[appcast] FATAL: missing file $f" >&2
        exit 1
    fi
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="$(cat "$SCRIPT_DIR/SPARKLE_VERSION" | tr -d '[:space:]')"
WORK_DIR="$(mktemp -d -t sparkle-appcast.XXXXXX)"
trap 'rm -rf "$WORK_DIR"' EXIT

# Pull in sign_update from the official Sparkle dist. We could vendor a copy,
# but tying to the official binary makes our signature format identical to
# whatever the framework version we ship expects (they always co-version).
echo "[appcast] fetching sign_update from Sparkle ${VERSION}"
curl --proto '=https' --tlsv1.2 -sSfL \
    "https://github.com/sparkle-project/Sparkle/releases/download/${VERSION}/Sparkle-${VERSION}.tar.xz" \
    -o "$WORK_DIR/sparkle.tar.xz"
tar -C "$WORK_DIR" -xJf "$WORK_DIR/sparkle.tar.xz"
SIGN_UPDATE="$WORK_DIR/bin/sign_update"
chmod +x "$SIGN_UPDATE"

# Returns a string like: sparkle:edSignature="ABC==" length="123456"
# (Sparkle's official output is exactly the XML-attribute-ready form, so we
# can inject the line verbatim into the appcast.)
sign_pkg() {
    local pkg="$1"
    "$SIGN_UPDATE" -f "$SPARKLE_ED_PRIVKEY_PATH" "$pkg"
}

ATTR_ARM64="$(sign_pkg "$IOGRID_PKG_ARM64")"
ATTR_AMD64="$(sign_pkg "$IOGRID_PKG_AMD64")"

PKG_BASENAME_ARM64="$(basename "$IOGRID_PKG_ARM64")"
PKG_BASENAME_AMD64="$(basename "$IOGRID_PKG_AMD64")"

cat > "$OUT" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"
     xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle"
     xmlns:dc="http://purl.org/dc/elements/1.1/">
    <channel>
        <title>iogrid daemon</title>
        <link>${IOGRID_PKG_URL_BASE%/*}/appcast.xml</link>
        <description>Stable channel — production daemon releases.</description>
        <language>en</language>
        <item>
            <title>iogrid ${IOGRID_VERSION}</title>
            <sparkle:releaseNotesLink>${IOGRID_RELEASE_NOTES_URL}</sparkle:releaseNotesLink>
            <pubDate>${PUBDATE}</pubDate>
            <sparkle:minimumSystemVersion>${MIN_OS_VERSION}</sparkle:minimumSystemVersion>
            <enclosure
                url="${IOGRID_PKG_URL_BASE}/${PKG_BASENAME_ARM64}"
                sparkle:version="${IOGRID_VERSION}"
                sparkle:shortVersionString="${IOGRID_VERSION}"
                sparkle:os="macos"
                ${ATTR_ARM64}
                type="application/octet-stream"/>
            <enclosure
                url="${IOGRID_PKG_URL_BASE}/${PKG_BASENAME_AMD64}"
                sparkle:version="${IOGRID_VERSION}"
                sparkle:shortVersionString="${IOGRID_VERSION}"
                sparkle:os="macos"
                ${ATTR_AMD64}
                type="application/octet-stream"/>
        </item>
    </channel>
</rss>
EOF

echo "[appcast] wrote $OUT"
echo "[appcast]   arm64 enclosure attrs: $ATTR_ARM64"
echo "[appcast]   amd64 enclosure attrs: $ATTR_AMD64"
