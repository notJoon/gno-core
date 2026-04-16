#!/bin/sh
# Gno precompiled binary installer (Linux/macOS, amd64/arm64).
# Run with --help for usage.

set -eu

REPO="gnolang/gno"
API="https://api.github.com/repos/${REPO}"
COMPONENTS="gno gnokey gnodev gnobro"

VERSION="${GNO_VERSION:-latest}"
INSTALL_DIR="${GNO_INSTALL_DIR:-${HOME}/.gno/bin}"
UNINSTALL=0

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
  --uninstall       remove installed binaries (including legacy source dir)
  --help            show this help

Environment:
  GNO_VERSION       same as --version
  GNO_INSTALL_DIR   same as --dir
EOF
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --version)   [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
            --dir)       [ $# -ge 2 ] || die "--dir needs a value"; INSTALL_DIR="$2"; shift 2 ;;
            --uninstall) UNINSTALL=1; shift ;;
            -h|--help)   show_help; exit 0 ;;
            *)           die "unknown flag: $1 (try --help)" ;;
        esac
    done
}

# env checks

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
    # Prefer jq for JSON parsing; fall back to awk (see asset_url).
    if command -v jq >/dev/null 2>&1; then JSON="jq"; else JSON="awk"; fi
}

# installation

# Emit .tag_name from the release metadata on stdout.
release_tag() {
    if [ "$JSON" = "jq" ]; then
        jq -r '.tag_name' "$TMP/release.json"
    else
        sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$TMP/release.json" | head -1
    fi
}

# Emit the API URL for asset $1 from the release metadata on stdout.
# Prefers jq; the awk fallback scans pretty-printed JSON and relies on the
# current field order ("url" before "name" within each asset).
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
    CURL="curl --proto =https --tlsv1.2 -fsSL --retry 3 --retry-delay 2"

    # The public github.com/<repo>/releases/download/... path currently 404s for
    # this repository; resolving assets via the API endpoint works around it.
    [ "$VERSION" = "latest" ] && REL="latest" || REL="tags/$VERSION"
    META_URL="${API}/releases/${REL}"

    TMP="$(mktemp -d)"
    trap 'rm -rf "$TMP"' EXIT INT TERM

    $CURL "$META_URL" > "$TMP/release.json" \
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
    $CURL -H "Accept: application/octet-stream" -o "$TMP/$ARCHIVE"      "$ARCHIVE_URL" || die "archive download failed"
    $CURL -H "Accept: application/octet-stream" -o "$TMP/checksums.txt" "$SUMS_URL"    || die "checksums download failed"

    expected="$(awk -v n="$ARCHIVE" '$2 == n {print $1; exit}' "$TMP/checksums.txt")"
    [ -n "$expected" ] || die "$ARCHIVE not listed in checksums.txt"
    actual="$(cd "$TMP" && $SHA "$ARCHIVE" | awk '{print $1}')"
    [ "$expected" = "$actual" ] || die "sha256 mismatch: expected $expected, got $actual"
    log "sha256 verified"

    mkdir -p "$INSTALL_DIR" "$TMP/ext"
    tar -xzf "$TMP/$ARCHIVE" -C "$TMP/ext"
    for c in $COMPONENTS; do
        [ -f "$TMP/ext/$c" ] || continue
        install -m 0755 "$TMP/ext/$c" "$INSTALL_DIR/$c"
        # Best-effort Gatekeeper unblock on macOS; harmless on Linux.
        [ "$OS" = "darwin" ] && xattr -d com.apple.quarantine "$INSTALL_DIR/$c" 2>/dev/null || true
    done

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
    gnokey add <name>           Create your key
    https://faucet.gno.land     Get testnet tokens
    https://docs.gno.land       Build and deploy your first package

  EOF
}

# Removes binaries from the current install dir, and — for users migrating
# from the previous source-build installer — also from $GOPATH/bin and the
# legacy ~/.gno/src source checkout.
uninstall_gno() {
    log "removing from $INSTALL_DIR"
    for c in gno gnokey gnodev gnobro gnoland gnoweb; do
        rm -f "$INSTALL_DIR/$c"
    done
    if command -v go >/dev/null 2>&1; then
        gobin="$(go env GOPATH 2>/dev/null)/bin"
        if [ "$gobin" != "/bin" ]; then
            log "removing legacy binaries from $gobin"
            for c in gno gnokey gnodev gnobro; do
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
    if [ "$UNINSTALL" = 1 ]; then
        uninstall_gno
        exit 0
    fi
    detect_platform
    check_deps
    install_gno
}

main "$@"
