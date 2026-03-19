#!/bin/bash
set -e

# Build the server bundle for distribution.
# The bundle contains compiled JS + node_modules (no Chromium — Puppeteer downloads it on first run).
# This is platform-independent since all deps are pure JS.

# Prevent Puppeteer from downloading Chromium during any npm install in this script
export PUPPETEER_SKIP_DOWNLOAD=true

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
VERSION="${1:-dev}"

echo "Building server bundle v${VERSION}..."

cd "$ROOT_DIR/server"

# Clean previous build artifacts
rm -rf dist generated

# Install all deps (need dev deps for proto generation and TypeScript compilation)
npm install

# Generate proto
npm run generate

# Build TypeScript
npm run build

# Create a clean bundle directory
BUNDLE_DIR=$(mktemp -d)
BUNDLE_NAME="termium-server-${VERSION}"
DEST="${BUNDLE_DIR}/${BUNDLE_NAME}"

mkdir -p "$DEST"

# Copy only what's needed at runtime
cp -r dist "$DEST/"
cp -r generated "$DEST/"
cp package.json "$DEST/"

# Install production-only deps into the bundle (clean, no dev deps)
cd "$DEST"
npm install --omit=dev

# Create the tarball
cd "$BUNDLE_DIR"
tar czf "${BUNDLE_NAME}.tar.gz" "$BUNDLE_NAME"

# Move to project dist/
mkdir -p "${ROOT_DIR}/dist"
mv "${BUNDLE_NAME}.tar.gz" "${ROOT_DIR}/dist/"

# Cleanup
rm -rf "$BUNDLE_DIR"

echo "Server bundle created: dist/${BUNDLE_NAME}.tar.gz"
echo "Size: $(du -h "${ROOT_DIR}/dist/${BUNDLE_NAME}.tar.gz" | cut -f1)"
