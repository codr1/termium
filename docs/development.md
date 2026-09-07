# Development

These instructions are for contributors building the current version. End-user installation is being redesigned around [one command](installation.md).

## Toolchain

Use Node.js 24 or newer (prefer an LTS release) and Go 1.23.1 or newer. Install `protoc` using your development environment's package manager. The build also needs the Go protobuf plugins:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
export PATH="$(go env GOPATH)/bin:$PATH"
```

The full automated suite has been verified on Linux AMD64 with Node 24.20.0 and Go 1.23.1 using the plugin versions above. The full suite also passes on the native Apple Silicon and Intel macOS CI runners. Node 18 and 20 are [end-of-life](https://nodejs.org/en/about/previous-releases).

## Build, install, and run

The installer, Vimium, and website changes are currently being reviewed on the development preview branch. To try that version before the first release, clone it with:

```bash
git clone --branch feat/public-website https://github.com/codr1/termium.git
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

`npm run build:bundle` creates `dist/termium-<os>-<arch>.tar.gz` and its mandatory `.sha256` file. It uses the npm lockfile, the pinned Node checksums in `scripts/runtime-lock.json`, and Puppeteer's matching Chromium revision. Production dependencies are installed with lifecycle scripts disabled. No development tool runs on an end user's machine.

`npm run test:installation` installs that archive into a fresh home with development commands removed from PATH, repeats setup, rejects a bad checksum, checks shell command discovery, runs the private browser, and browses through a PTY.

The Package workflow builds and exercises each native archive on Linux x86-64, macOS ARM64, and macOS AMD64. A version tag runs the full tests and package checks before creating a **draft** GitHub release. Review the artifacts and clean-machine acceptance evidence before publishing it.

[Documentation home](README.md)
