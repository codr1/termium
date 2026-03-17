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
    if [ "$node_version" -lt 18 ]; then
        die "Node.js 18+ required, found $(node -v)"
    fi

    if ! command -v curl &>/dev/null && ! command -v wget &>/dev/null; then
        die "curl or wget is required"
    fi
}

# Download a file
download() {
    local url="$1" dest="$2"
    if command -v curl &>/dev/null; then
        curl -fsSL "$url" -o "$dest"
    else
        wget -q "$url" -O "$dest"
    fi
}

# Get latest release version from GitHub
get_latest_version() {
    local version
    version=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"//;s/".*//')
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
    # Strip leading 'v' for filenames
    local version_stripped="${version#v}"
    info "Version: ${version}"

    # Create install directories
    mkdir -p "$BIN_DIR" "$SERVER_DIR"

    # Download client binary
    local client_url="https://github.com/${REPO}/releases/download/${version}/termium-client-${version_stripped}-${platform}.tar.gz"
    info "Downloading client..."
    local tmp_client
    tmp_client=$(mktemp)
    download "$client_url" "$tmp_client"
    tar xzf "$tmp_client" -C "$BIN_DIR" --strip-components=1
    chmod +x "${BIN_DIR}/termium"
    rm -f "$tmp_client"
    info "Client installed to ${BIN_DIR}/termium"

    # Download server bundle
    local server_url="https://github.com/${REPO}/releases/download/${version}/termium-server-${version_stripped}.tar.gz"
    info "Downloading server..."
    local tmp_server
    tmp_server=$(mktemp)
    download "$server_url" "$tmp_server"
    rm -rf "$SERVER_DIR"
    mkdir -p "$SERVER_DIR"
    tar xzf "$tmp_server" -C "$SERVER_DIR" --strip-components=1
    rm -f "$tmp_server"
    info "Server installed to ${SERVER_DIR}"

    # First run will trigger Puppeteer's Chromium download.
    # We could do it now, but it's ~300MB and the user might want to know it's happening.
    warn "Note: First run will download Chromium (~300MB). This is a one-time download."

    # Check if BIN_DIR is in PATH
    echo
    if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
        warn "Add termium to your PATH by adding this to your shell profile:"
        echo
        echo "  export PATH=\"${BIN_DIR}:\$PATH\""
        echo

        # Detect shell and suggest the right file
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
