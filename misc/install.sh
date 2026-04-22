#!/bin/sh
# Gno precompiled binary installer (Linux/macOS, amd64/arm64).
# Run with --help for usage.

set -eu

REPO="gnolang/gno"
API="https://api.github.com/repos/${REPO}"

COMPONENTS="gno gnokey gnodev gnobro gnoweb"
FULL_COMPONENTS="gno gnokey gnodev gnobro gnoweb gnoland"

VERSION="${GNO_VERSION:-latest}"
INSTALL_DIR="${GNO_INSTALL_DIR:-${HOME}/.gno/bin}"
UNINSTALL=0
FULL=0

log() { printf '[gno-install] %s\n' "$1"; }
die() { printf '[gno-install] error: %s\n' "$1" >&2; exit 1; }

show_help() {
    cat <<'EOF'
Gno precompiled binary installer (Linux/macOS, amd64/arm64).

Usage:
  curl --proto '=https' --tlsv1.2 -sSf \
    https://raw.githubusercontent.com/gnolang/gno/master/misc/install.sh | sh

Flags:
  --version <tag>   install a specific release tag (default: latest)
  --dir <path>      install directory (default: $HOME/.gno/bin)
  --full            also install the validator node (gnoland)
  --uninstall       remove installed binaries (including legacy source dir)
  --help            show this help

By default installs: gno, gnokey, gnodev, gnobro, gnoweb.
Use --full to additionally install gnoland (validator node).

Environment:
  GNO_VERSION       same as --version
  GNO_INSTALL_DIR   same as --dir
  GITHUB_TOKEN      optional. authenticates GitHub API requests to raise the
                    60 requests/hour anonymous rate limit
EOF
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --version)   [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
            --dir)       [ $# -ge 2 ] || die "--dir needs a value"; INSTALL_DIR="$2"; shift 2 ;;
            --full)      FULL=1; shift ;;
            --uninstall) UNINSTALL=1; shift ;;
            -h|--help)   show_help; exit 0 ;;
            *)           die "unknown flag: $1 (try --help)" ;;
        esac
    done
}

detect_platform() {
    case "$(uname -s)" in
        Linux)  OS="linux" ;;
        Darwin) OS="darwin" ;;
        *) die "unsupported OS: $(uname -s)" ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64)  ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *) die "unsupported architecture: $(uname -m)" ;;
    esac
}

check_deps() {
    command -v curl    >/dev/null 2>&1 || die "curl is required"
    command -v tar     >/dev/null 2>&1 || die "tar is required"
    command -v install >/dev/null 2>&1 || die "install is required"

    if   command -v sha256sum >/dev/null 2>&1; then SHA="sha256sum"
    elif command -v shasum    >/dev/null 2>&1; then SHA="shasum -a 256"
    else die "sha256sum or shasum is required"

    fi
    if command -v jq >/dev/null 2>&1; then JSON="jq"; else JSON="awk"; fi
}

# Stack-based xtrace suspension; safe across nested callers and subshells.
suspend_xtrace() {
    case "$-" in
        *x*) _xt_stack="1${_xt_stack-}"; set +x ;;
        *)   _xt_stack="0${_xt_stack-}" ;;
    esac
}

restore_xtrace() {
    case "${_xt_stack-}" in
        1*) _xt_stack="${_xt_stack#?}"; set -x ;;
        0*) _xt_stack="${_xt_stack#?}" ;;
        *)  : ;;
    esac
}

# Do not use for asset downloads: asset URLs redirect to another host and
# curl headers survive cross-host redirects.
api_get() {
    _headers="$TMP/api_headers"
    if [ -n "${GH_API_TOKEN:+x}" ]; then
        suspend_xtrace
        printf 'header = "Authorization: Bearer %s"\n' "$GH_API_TOKEN" \
            | $CURL -D "$_headers" --config - "$@"
        _rc=$?
        restore_xtrace
    else
        $CURL -D "$_headers" "$@"
        _rc=$?
    fi
    if [ "$_rc" -ne 0 ] && [ -z "${GH_API_TOKEN:+x}" ] && [ -f "$_headers" ] \
        && grep -qi '^x-ratelimit-remaining:[[:space:]]*0[[:space:]]*$' "$_headers" 2>/dev/null; then
        log "GitHub API rate limit exhausted (60/hour anonymous)" >&2
        log "set GITHUB_TOKEN to authenticate; see --help" >&2
    fi
    return $_rc
}

# Intentionally omits -L: we need the redirect target (signed URL), not
# the asset content, and Authorization must not reach the CDN host.
resolve_asset() {
    if [ -n "${GH_API_TOKEN:+x}" ]; then
        suspend_xtrace
        printf 'header = "Authorization: Bearer %s"\n' "$GH_API_TOKEN" \
            | $CURL --config - \
                -H "Accept: application/octet-stream" \
                -o /dev/null -w '%{redirect_url}' \
                "$1"
        _rc=$?
        restore_xtrace
        return $_rc
    fi
    $CURL \
        -H "Accept: application/octet-stream" \
        -o /dev/null -w '%{redirect_url}' \
        "$1"
}

# Move GITHUB_TOKEN into a non-exported variable before spawning children.
capture_github_token() {
    GH_API_TOKEN=
    if [ -n "${GITHUB_TOKEN:+x}" ]; then
        suspend_xtrace
        GH_API_TOKEN=$GITHUB_TOKEN
        unset GITHUB_TOKEN
        restore_xtrace
    fi
}

release_tag() {
    if [ "$JSON" = "jq" ]; then
        jq -r '.tag_name' "$TMP/release.json"
    else
        sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$TMP/release.json" | head -1
    fi
}

# /releases/latest may point at a chain/* tag with no binaries, so we walk
# the list and pick the first non-prerelease goreleaser-built tag.
latest_v_tag() {
    if [ "$JSON" = "jq" ]; then
        jq -r 'map(select(.prerelease == false and (.tag_name | startswith("v")))) | .[0].tag_name // empty' "$TMP/releases.json"
    else
        # GitHub lists releases newest-first. Pair each v-prefixed tag_name with
        # the prerelease flag that follows it in the same release object, and
        # emit the first non-prerelease one.
        awk '
            /"tag_name":/ {
                line = $0
                sub(/.*"tag_name"[[:space:]]*:[[:space:]]*"/, "", line)
                sub(/".*/, "", line)
                if (line ~ /^v/) tag = line; else tag = ""
                next
            }
            /"prerelease":/ {
                if (tag != "" && $0 !~ /true/) { print tag; exit }
                tag = ""
            }
        ' "$TMP/releases.json"
    fi
}

# The awk fallback scans pretty-printed JSON and relies on the current
# field order ("url" before "name" within each asset).
asset_url() {
    if [ "$JSON" = "jq" ]; then
        jq -r --arg n "$1" '.assets[] | select(.name == $n) | .url' "$TMP/release.json"
    else
        awk -v t="$1" -v api="$API" '
            match($0, /releases\/assets\/[0-9]+/) { u = substr($0, RSTART, RLENGTH) }
            /"name":/ && index($0, "\"" t "\"") && u { print api "/" u; exit }
        ' "$TMP/release.json"
    fi
}

install_gno() {
    # --proto =https and --tlsv1.2 harden the transport; --retry handles flaky nets.
    # -L is added per-call: resolve_asset needs the redirect URL (no follow),
    # downloads of signed URLs follow redirects explicitly.
    CURL="curl --proto =https --tlsv1.2 -fsS --retry 3 --retry-delay 2"

    if [ -n "${GH_API_TOKEN:+x}" ]; then
        log "authenticating GitHub API requests with GITHUB_TOKEN"
    fi

    TMP="$(mktemp -d)"
    trap 'rm -rf "$TMP"' EXIT INT TERM

    # GitHub's /releases/latest resolves to whatever it ranks as "latest",
    # which for this repo is a chain/* tag without binaries. Resolve "latest"
    # to the most recent v* tag ourselves instead.
    if [ "$VERSION" = "latest" ]; then
        api_get "${API}/releases?per_page=30" > "$TMP/releases.json" \
            || die "failed to fetch releases list"
        VERSION="$(latest_v_tag)"
        [ -n "$VERSION" ] || die "no v* release found; pass --version <tag> explicitly (see https://github.com/${REPO}/releases)"
    fi

    # The public github.com/<repo>/releases/download/... path currently 404s for
    # this repository; resolving assets via the API endpoint works around it.
    META_URL="${API}/releases/tags/${VERSION}"
    api_get "$META_URL" > "$TMP/release.json" \
        || die "failed to fetch release metadata ($VERSION)"

    VERSION="$(release_tag)"
    [ -n "$VERSION" ] || die "could not parse tag_name from release metadata"

    ARCHIVE="gno_${VERSION#v}_${OS}_${ARCH}.tar.gz"
    log "installing gno ${VERSION} (${OS}/${ARCH}) into ${INSTALL_DIR}"

    ARCHIVE_URL="$(asset_url "$ARCHIVE")"
    SUMS_URL="$(asset_url "checksums.txt")"
    [ -n "$ARCHIVE_URL" ] || die "$ARCHIVE is not an asset of $VERSION (no binaries for ${OS}/${ARCH}?)"
    [ -n "$SUMS_URL" ]    || die "checksums.txt missing from $VERSION"

    log "downloading $ARCHIVE"
    # Signed CDN URLs carry short-lived query credentials; keep them out of xtrace.
    suspend_xtrace
    ARCHIVE_SIGNED="$(resolve_asset "$ARCHIVE_URL")"
    [ -n "$ARCHIVE_SIGNED" ] || die "could not resolve $ARCHIVE download URL"
    $CURL -L -o "$TMP/$ARCHIVE"      "$ARCHIVE_SIGNED" || die "archive download failed"

    SUMS_SIGNED="$(resolve_asset "$SUMS_URL")"
    [ -n "$SUMS_SIGNED" ] || die "could not resolve checksums.txt download URL"
    $CURL -L -o "$TMP/checksums.txt" "$SUMS_SIGNED"    || die "checksums download failed"
    restore_xtrace

    expected="$(awk -v n="$ARCHIVE" '$2 == n {print $1; exit}' "$TMP/checksums.txt")"
    [ -n "$expected" ] || die "$ARCHIVE not listed in checksums.txt"
    actual="$(cd "$TMP" && $SHA "$ARCHIVE" | awk '{print $1}')"
    [ "$expected" = "$actual" ] || die "sha256 mismatch: expected $expected, got $actual"
    log "sha256 verified"

    mkdir -p "$INSTALL_DIR" "$TMP/ext"
    tar -xzf "$TMP/$ARCHIVE" -C "$TMP/ext"
    if [ "$FULL" = 1 ]; then
        components="$FULL_COMPONENTS"
    else
        components="$COMPONENTS"
    fi
    missing=""
    installed_count=0
    for c in $components; do
        if [ ! -f "$TMP/ext/$c" ]; then
            missing="${missing} ${c}"
            continue
        fi
        install -m 0755 "$TMP/ext/$c" "$INSTALL_DIR/$c"
        installed_count=$((installed_count + 1))
        # Best-effort Gatekeeper unblock on macOS; harmless on Linux.
        [ "$OS" = "darwin" ] && xattr -d com.apple.quarantine "$INSTALL_DIR/$c" 2>/dev/null || true
    done
    [ "$installed_count" -gt 0 ] || die "no expected binaries found in $ARCHIVE (missing:${missing})"
    [ -z "$missing" ] || log "warning: expected binaries missing from $ARCHIVE:${missing}"

    log "installed into $INSTALL_DIR"

    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *) log "add to PATH: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
    esac

    cat <<'EOF'

                    __             __
  ___ ____  ___    / /__ ____  ___/ /
 / _ `/ _ \/ _ \_ / / _ `/ _ \/ _  /
 \_, /_//_/\___(_)_/\_,_/_//_/\_,_//
/___/

 To get started:
    gnodev                      Run a local chain with hot reload
    https://gno.land            Explore realms already deployed on-chain
    https://docs.gno.land       Full guide + deploy to a live network

EOF
}

# Removes binaries from the current install dir, and — for users migrating
# from the previous source-build installer — also from $GOPATH/bin and the
# legacy ~/.gno/src source checkout.
uninstall_gno() {
    log "removing from $INSTALL_DIR"
    for c in $FULL_COMPONENTS; do
        rm -f "$INSTALL_DIR/$c"
    done
    if command -v go >/dev/null 2>&1; then
        gobin="$(go env GOPATH 2>/dev/null)/bin"
        if [ "$gobin" != "/bin" ]; then
            log "removing legacy binaries from $gobin"
            for c in $FULL_COMPONENTS; do
                rm -f "$gobin/$c"
            done
        fi
    fi
    if [ -d "$HOME/.gno/src" ]; then
        log "removing legacy source dir $HOME/.gno/src"
        rm -rf "$HOME/.gno/src"
    fi
    log "uninstalled"
}

main() {
    parse_args "$@"
    capture_github_token
    if [ "$UNINSTALL" = 1 ]; then
        uninstall_gno
        exit 0
    fi
    detect_platform
    check_deps
    install_gno
}

main "$@"
