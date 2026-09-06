#!/bin/bash
set -e

# Termium installer
# Usage: curl -fsSL https://raw.githubusercontent.com/codr1/termium/main/scripts/install.sh | bash

REPO="codr1/termium"
INSTALL_DIR="${TERMIUM_HOME:-$HOME/.termium}"
BIN_DIR="${INSTALL_DIR}/bin"
SERVER_DIR="${INSTALL_DIR}/server"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[termium]${NC} $*"; }
warn()  { echo -e "${YELLOW}[termium]${NC} $*"; }
error() { echo -e "${RED}[termium]${NC} $*" >&2; }
die()   { error "$@"; exit 1; }

# Detect platform
detect_platform() {
    local os arch

    case "$(uname -s)" in
        Linux*)  os="linux" ;;
        Darwin*) os="darwin" ;;
        *)       die "Unsupported OS: $(uname -s)" ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)  arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *)             die "Unsupported architecture: $(uname -m)" ;;
    esac

    echo "${os}-${arch}"
}

# Check prerequisites
check_prereqs() {
    if ! command -v node &>/dev/null; then
        die "Node.js is required but not found. Install it from https://nodejs.org/"
    fi

    local node_version
    node_version=$(node -v | sed 's/v//' | cut -d. -f1)
    if [ "$node_version" -lt 24 ]; then
        die "Node.js 24+ required, found $(node -v)"
    fi

    if ! command -v curl &>/dev/null && ! command -v wget &>/dev/null; then
        die "curl or wget is required"
    fi

    if ! command -v sha256sum &>/dev/null && ! command -v shasum &>/dev/null; then
        warn "sha256sum/shasum not found — skipping checksum verification"
    fi
}

# Download a file (works with curl or wget)
download() {
    local url="$1" dest="$2"
    if command -v curl &>/dev/null; then
        curl -fsSL "$url" -o "$dest"
    elif command -v wget &>/dev/null; then
        wget -q "$url" -O "$dest"
    else
        die "Neither curl nor wget found"
    fi
}

# Download a URL to stdout (for API calls)
download_stdout() {
    local url="$1"
    if command -v curl &>/dev/null; then
        curl -fsSL "$url"
    elif command -v wget &>/dev/null; then
        wget -q "$url" -O -
    else
        die "Neither curl nor wget found"
    fi
}

# Verify SHA256 checksum
verify_checksum() {
    local file="$1" expected="$2"
    local actual

    if command -v sha256sum &>/dev/null; then
        actual=$(sha256sum "$file" | cut -d' ' -f1)
    elif command -v shasum &>/dev/null; then
        actual=$(shasum -a 256 "$file" | cut -d' ' -f1)
    else
        warn "Skipping checksum verification (no sha256sum or shasum)"
        return 0
    fi

    if [ "$actual" != "$expected" ]; then
        die "Checksum mismatch for $(basename "$file"): expected $expected, got $actual"
    fi
}

# Get latest release version from GitHub
get_latest_version() {
    local version
    version=$(download_stdout "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"//;s/".*//')
    if [ -z "$version" ]; then
        die "Failed to determine latest version. Check https://github.com/${REPO}/releases"
    fi
    echo "$version"
}

main() {
    info "Installing termium..."
    echo

    check_prereqs

    local platform version
    platform=$(detect_platform)
    info "Detected platform: ${platform}"

    # Get version (from arg or latest release)
    version="${1:-$(get_latest_version)}"
    local version_stripped="${version#v}"
    info "Version: ${version}"

    # Create install directories
    mkdir -p "$BIN_DIR" "$SERVER_DIR"

    # Download checksums
    local checksums_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"
    local tmp_checksums
    tmp_checksums=$(mktemp)
    info "Downloading checksums..."
    if ! download "$checksums_url" "$tmp_checksums" 2>/dev/null; then
        warn "Checksums not available — skipping verification"
        echo "" > "$tmp_checksums"
    fi

    # Download and verify client binary
    local client_tarball="termium-client-${version_stripped}-${platform}.tar.gz"
    local client_url="https://github.com/${REPO}/releases/download/${version}/${client_tarball}"
    info "Downloading client..."
    local tmp_client
    tmp_client=$(mktemp)
    download "$client_url" "$tmp_client"

    local expected_checksum
    expected_checksum=$(grep "${client_tarball}" "$tmp_checksums" | cut -d' ' -f1)
    if [ -n "$expected_checksum" ]; then
        verify_checksum "$tmp_client" "$expected_checksum"
        info "Client checksum verified"
    fi

    tar xzf "$tmp_client" -C "$BIN_DIR"
    # GoReleaser puts the binary inside a directory; find and move it
    find "$BIN_DIR" -name "termium" -type f -exec mv {} "$BIN_DIR/termium" \;
    find "$BIN_DIR" -mindepth 1 -type d -exec rm -rf {} + 2>/dev/null || true
    chmod +x "${BIN_DIR}/termium"
    rm -f "$tmp_client"
    info "Client installed to ${BIN_DIR}/termium"

    # Download and verify server bundle
    local server_tarball="termium-server-${version_stripped}.tar.gz"
    local server_url="https://github.com/${REPO}/releases/download/${version}/${server_tarball}"
    info "Downloading server..."
    local tmp_server
    tmp_server=$(mktemp)
    download "$server_url" "$tmp_server"

    expected_checksum=$(grep "${server_tarball}" "$tmp_checksums" | cut -d' ' -f1)
    if [ -n "$expected_checksum" ]; then
        verify_checksum "$tmp_server" "$expected_checksum"
        info "Server checksum verified"
    fi

    rm -rf "$SERVER_DIR"
    mkdir -p "$SERVER_DIR"
    tar xzf "$tmp_server" -C "$SERVER_DIR" --strip-components=1
    rm -f "$tmp_server"
    rm -f "$tmp_checksums"
    info "Server installed to ${SERVER_DIR}"

    # Launching Puppeteer does not install a missing browser.
    warn "Browser provisioning is not implemented by this installer. See https://github.com/codr1/termium/blob/main/docs/installation.md"

    # Check if BIN_DIR is in PATH
    echo
    if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
        warn "Add termium to your PATH by adding this to your shell profile:"
        echo
        echo "  export PATH=\"${BIN_DIR}:\$PATH\""
        echo

        local shell_rc
        case "$(basename "$SHELL")" in
            zsh)  shell_rc="$HOME/.zshrc" ;;
            bash) shell_rc="$HOME/.bashrc" ;;
            fish) shell_rc="$HOME/.config/fish/config.fish" ;;
            *)    shell_rc="your shell profile" ;;
        esac
        warn "For $(basename "$SHELL"), add it to ${shell_rc}"
    fi

    info "Installation complete!"
    echo
    info "Run 'termium' to start browsing."
    info "For Ghostty/Kitty: termium --renderer kitty"
}

main "$@"
