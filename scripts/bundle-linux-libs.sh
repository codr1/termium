#!/bin/sh
# Runs only inside the pinned Debian build container, never on the user's host.
set -eu
apt-get update -qq
apt-get satisfy -y --no-install-recommends "$(paste -sd, /bundle/browser/chrome-linux64/deb.deps)"
apt-get install -y --no-install-recommends fonts-dejavu-core fonts-noto-core fonts-noto-color-emoji fonts-noto-cjk
mkdir -p /bundle/lib /bundle/notices/debian /bundle/fonts
# NSS loads these modules dynamically, so the browser's ldd output omits them.
# Mixing a bundled NSS with newer host modules can abort on the first page.
modules='/usr/lib/x86_64-linux-gnu/libsoftokn3.so /usr/lib/x86_64-linux-gnu/libfreebl3.so /usr/lib/x86_64-linux-gnu/libfreeblpriv3.so /usr/lib/x86_64-linux-gnu/libnssckbi.so'
ldd /bundle/browser/chrome-linux64/chrome /bundle/runtime/bin/node $modules > /tmp/termium-ldd
if grep -q 'not found' /tmp/termium-ldd; then cat /tmp/termium-ldd; exit 1; fi
awk '/=> \// { print $3 }' /tmp/termium-ldd | sort -u | while IFS= read -r library; do
    name=$(basename "$library")
    # libc and its companion libraries must come from the host's matching
    # loader. Everything else is built against the declared glibc 2.36 floor.
    case "$name" in
        libc.so.*|libm.so.*|libpthread.so.*|libdl.so.*|librt.so.*|libresolv.so.*|libutil.so.*|libnss_*.so.*) continue ;;
    esac
    cp -L "$library" "/bundle/lib/$name"
done
for library in $modules; do cp -L "$library" "/bundle/lib/$(basename "$library")"; done
cp -R /usr/share/fonts/truetype /usr/share/fonts/opentype /bundle/fonts/
for doc in /usr/share/doc/*; do
    if [ -f "$doc/copyright" ]; then
        mkdir -p "/bundle/notices/debian/$(basename "$doc")"
        cp -L "$doc/copyright" "/bundle/notices/debian/$(basename "$doc")/copyright"
    fi
done
dpkg-query -W > /bundle/notices/debian/packages.txt
chown -R "$TERMIUM_BUILD_UID:$TERMIUM_BUILD_GID" /bundle/lib /bundle/fonts /bundle/notices
