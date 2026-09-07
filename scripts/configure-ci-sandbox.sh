#!/bin/bash
# Disposable GitHub runner setup, never part of the distributed installer.
set -euo pipefail
[ "${GITHUB_ACTIONS:-}" = true ] || { echo 'This script is only for GitHub Actions runners.' >&2; exit 1; }
# Ubuntu restricts user namespaces for otherwise unconfined applications.
# Permit our downloaded Chrome executables while retaining Chromium's own
# namespace and seccomp sandboxes. Do not change the global AppArmor policy.
# https://chromium.googlesource.com/chromium/src/+/main/docs/security/apparmor-userns-restrictions.md
sudo tee /etc/apparmor.d/termium-ci >/dev/null <<'PROFILE'
abi <abi/4.0>,
include <tunables/global>
profile termium-ci /{tmp,home/runner}/**/chrome flags=(unconfined) {
  userns,
}
PROFILE
sudo apparmor_parser -r /etc/apparmor.d/termium-ci
