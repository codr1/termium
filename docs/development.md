# Development

These instructions are for contributors building the current version. End users can start with [Quickstart](quickstart.md).

## Toolchain

Use the Node.js and Go requirements declared in [package.json](../package.json) and [go.mod](../go.mod). CI uses the toolchains configured in [the test workflow](../.github/workflows/test.yml). Install `protoc` using your development environment’s package manager, then install the pinned Go protobuf plugins:

```bash
npm run setup:proto
export PATH="$(go env GOPATH)/bin:$PATH"
```

Dependency and tool versions live in manifests and build scripts so updates do not require editing this guide.

## Build, install, and run

Clone the main branch:

```bash
git clone https://github.com/codr1/termium.git
cd termium
```

After cloning the repository and installing the toolchain above:

```bash
PUPPETEER_SKIP_DOWNLOAD=true npm ci
npm run install:local
termium example.com
```

`install:local` builds a normal client, assembles the native release bundle, checks its private browser, and installs it for your user. Linux packaging uses Docker with the pinned Debian build image to collect libraries and fonts; it does not install packages on your host. The installed command is independent of your checkout. Rebuild and reinstall when you want to update that copy.

For an edit/test loop without installing, `npm run build` builds both components and `npm run test:browser:install` prepares the development browser. The development binary remains at `client/termium`; the installed command is the normal daily entry point.

## Rebuild and test

```bash
npm test
```

This rebuilds both components, runs static checks and Go tests with the race detector, prepares Chrome, and runs real browser integration tests. See [Testing](testing.md) for focused commands, CI coverage, and the remaining terminal and installer checks.

Generated protobuf code is not tracked; `npm test` generates it before running tests. To rebuild just the client after generation, use `npm run build:client`. The tests use isolated temporary resources and can run alongside a development session.

## Debugging

```bash
./client/termium --debug --logfile termium-debug.log --splash NONE
./client/termium --renderer sixel --palette websafe --timings
./client/termium --cpuprofile termium.prof
```

These are separate diagnostic examples. Logs may include URLs and typed text.

To run the server independently, use `npm run start:server`. `TERMIUM_SERVER` overrides the client discovery path and must point to `server.js`. The normal search order is that override, `../server/dist/src/server.js` relative to the resolved client binary, then `~/.termium/server/dist/src/server.js`.

Normal clients create private per-session Unix sockets. A manually started server uses `/tmp/termium.sock` unless given `--socket`. TCP is unencrypted and unauthenticated; keep manual experiments local. Chromium launches with its sandbox enabled.

## Release packages

`npm run build:bundle` creates `dist/termium-<os>-<arch>.tar.gz` and its mandatory `.sha256` file. It uses the npm lockfile, the pinned Node checksums in `scripts/runtime-lock.json`, and Puppeteer's matching Chromium revision. Production dependencies are installed with lifecycle scripts disabled. `scripts/build-dependency-manifest.mjs` records upstream Chromium and Vimium ZIP URLs and checksums in `bundle.json`. It also fingerprints the reviewed Vimium source files; installation requires an exact byte match and excludes unreviewed archive files. The final app archive omits those dependencies and keeps Termium’s welcome-page overlay; the installer downloads and verifies the dependencies before validation and activation. No development tool runs on an end user's machine.

`npm run test:installation` installs that archive into a fresh home with development commands removed from PATH, repeats setup, rejects a bad checksum, checks shell command discovery, runs the private browser, and browses through a PTY.

The Package workflow builds and exercises each native archive on Linux x86-64, macOS ARM64, and macOS AMD64. A version tag runs the full tests and package checks before creating a **draft** GitHub release. Review the artifacts and clean-machine acceptance evidence before publishing it.

[Documentation home](README.md)
