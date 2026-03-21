#!/bin/bash
# Integration tests for the termium binary.
# Run from project root: bash tests/binary_test.sh

set -e

BINARY="client/termium"
PASS=0
FAIL=0

pass() { echo "  PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL + 1)); }

echo "Running binary integration tests..."
echo

# Ensure binary exists
if [ ! -x "$BINARY" ]; then
    echo "ERROR: $BINARY not found. Run 'npm run build' first."
    exit 1
fi

# Test: --version flag
echo "Test: --version"
output=$($BINARY --version 2>&1)
if echo "$output" | grep -q "termium"; then
    pass "--version prints version info"
else
    fail "--version output unexpected: $output"
fi

# Test: -v shorthand
echo "Test: -v shorthand"
output=$($BINARY -v 2>&1)
if echo "$output" | grep -q "termium"; then
    pass "-v prints version info"
else
    fail "-v output unexpected: $output"
fi

# Test: invalid renderer
echo "Test: invalid renderer"
output=$($BINARY --renderer bogus 2>&1) && rc=$? || rc=$?
if [ $rc -ne 0 ]; then
    pass "invalid renderer exits with error"
else
    fail "invalid renderer should exit with error"
fi

# Test: valid renderers accepted (will fail to connect to server, but shouldn't fail on config)
for renderer in sixel kitty tcell; do
    echo "Test: --renderer $renderer"
    output=$(timeout 2 $BINARY --renderer "$renderer" --splash NONE 2>&1) && rc=$? || rc=$?
    # Should not fail due to config parsing (exit code from timeout is 124, from connection failure is 1)
    if echo "$output" | grep -q "invalid renderer"; then
        fail "--renderer $renderer rejected as invalid"
    else
        pass "--renderer $renderer accepted"
    fi
done

# Test: --help flag
echo "Test: --help"
output=$($BINARY --help 2>&1) && rc=$? || rc=$?
if echo "$output" | grep -q "renderer"; then
    pass "--help shows renderer flag"
else
    fail "--help missing renderer flag"
fi

# Test: install.sh syntax
echo "Test: install.sh syntax"
if bash -n scripts/install.sh 2>&1; then
    pass "install.sh has valid bash syntax"
else
    fail "install.sh has syntax errors"
fi

# Test: build-server-bundle.sh syntax
echo "Test: build-server-bundle.sh syntax"
if bash -n scripts/build-server-bundle.sh 2>&1; then
    pass "build-server-bundle.sh has valid bash syntax"
else
    fail "build-server-bundle.sh has syntax errors"
fi

echo
echo "Results: $PASS passed, $FAIL failed"
[ $FAIL -eq 0 ] || exit 1
