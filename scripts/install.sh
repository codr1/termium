#!/bin/bash
# Bootstrap only: downloads and verifies the native app. The verified
# Go binary owns validation, activation, locking, and shell integration.
# Parse the complete function before running any installation steps when piped.
main() {
set -euo pipefail

fail() { printf 'termium: %s\n' "$*" >&2; exit 1; }
info() { printf '→ %s\n' "$*" >&2; }
archive=''
checksum=''
version='latest'
launch=1
while [ "$#" -gt 0 ]; do
    case "$1" in
        --archive) [ "$#" -ge 2 ] || fail '--archive needs a file'; archive=$2; shift 2 ;;
        --checksum) [ "$#" -ge 2 ] || fail '--checksum needs a SHA-256'; checksum=$2; shift 2 ;;
        --version) [ "$#" -ge 2 ] || fail '--version needs a release tag'; version=$2; shift 2 ;;
        --no-launch) launch=0; shift ;;
        --help) printf '%s\n' 'Install Termium: bash install.sh [--version TAG] [--no-launch]' 'Local release: bash install.sh --archive FILE --checksum SHA256' 'Opens Termium after setup when attached to a terminal, unless --no-launch is given.'; exit 0 ;;
        *) fail "Unknown argument: $1" ;;
    esac
done
case "$(uname -s)-$(uname -m)" in
    Linux-x86_64) platform=linux-amd64 ;;
    Darwin-arm64) platform=darwin-arm64 ;;
    Darwin-x86_64) platform=darwin-amd64 ;;
    Linux-aarch64|Linux-arm64) fail 'Linux ARM64 browser packaging is not available yet.' ;;
    *) fail 'This release supports Linux x86-64 and macOS Intel/Apple Silicon.' ;;
esac
if [ "$platform" = linux-amd64 ]; then
    libc=$(getconf GNU_LIBC_VERSION 2>/dev/null) || fail 'Linux needs glibc 2.36 or newer; musl is not supported.'
    libc=${libc#glibc }
    major=${libc%%.*}; minor=${libc#*.}; minor=${minor%%.*}
    if [ "$major" -lt 2 ] || { [ "$major" -eq 2 ] && [ "$minor" -lt 36 ]; }; then fail 'Linux needs glibc 2.36 or newer.'; fi
fi
command -v tar >/dev/null || fail 'tar is required.'
if command -v sha256sum >/dev/null; then
    sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null; then
    sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
    fail 'SHA-256 verification is unavailable; no files were installed.'
fi
work=$(mktemp -d "${TMPDIR:-/tmp}/termium-install.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if [ -z "$archive" ]; then
    command -v curl >/dev/null || fail 'curl is required.'
    case "$version" in *[!a-zA-Z0-9._-]*|'') fail 'Invalid release tag.' ;; esac
    base='https://github.com/codr1/termium/releases/latest/download'
    if [ "$version" != latest ]; then base="https://github.com/codr1/termium/releases/download/$version"; fi
    name="termium-$platform.tar.gz"
    info "Downloading Termium for $platform (Chromium and Vimium follow automatically)"
    curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 15 "$base/$name.sha256" -o "$work/checksum"
    checksum=$(awk -v name="$name" '$2 == name && length($1) == 64 { print $1 }' "$work/checksum")
    archive="$work/$name"
    curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 15 --progress-bar "$base/$name" -o "$archive"
fi
case "$checksum" in *[!a-f0-9]*|'') fail 'A valid archive SHA-256 is required.' ;; esac
[ "${#checksum}" -eq 64 ] || fail 'A valid archive SHA-256 is required.'
info 'Verifying release integrity'
[ "$(sha256 "$archive")" = "$checksum" ] || fail 'Checksum mismatch; your existing installation was not changed.'
# Reject absolute paths and traversal before extraction. Release checksums are
# mandatory, including for a local archive supplied explicitly by the user.
tar tzf "$archive" > "$work/members"
if ! awk '/^\// { exit 1 } { n=split($0,a,"/"); for(i=1;i<=n;i++) if(a[i]=="..") exit 1 }' "$work/members"; then fail 'Unsafe archive paths.'; fi
mkdir "$work/app"
tar xzf "$archive" -C "$work/app"
[ -x "$work/app/bin/termium" ] || fail 'Release does not contain an executable Termium client.'
TERMIUM_INSTALL_SHA256="$checksum" "$work/app/bin/termium" --install-bundle </dev/null
# The script arrives on stdin; the browser needs the user's actual keyboard.
# Never open the UI for redirected output or without a controlling terminal.
rm -rf -- "$work"
trap - EXIT INT TERM
if [ "$launch" -eq 1 ] && [ -t 1 ] && { true </dev/tty; } 2>/dev/null; then
    info 'Opening Termium. In new terminals, run termium from any directory.'
    exec "$HOME/.local/bin/termium" --first-run </dev/tty
fi
}

main "$@"
