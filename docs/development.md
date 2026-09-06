# Development

These instructions are for contributors building the current version. End-user installation is being redesigned around [one command](installation.md).

## Toolchain

Use Node.js 24 or newer (prefer an LTS release) and Go 1.23.1 or newer. Install `protoc` using your development environment's package manager. The build also needs the Go protobuf plugins:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
export PATH="$(go env GOPATH)/bin:$PATH"
```

The full automated suite has been verified on Linux AMD64 with Node 24.20.0 and Go 1.23.1 using the plugin versions above. Native macOS execution is configured in CI and still needs a successful hosted run. Node 18 and 20 are [end-of-life](https://nodejs.org/en/about/previous-releases).

## Build and run

```bash
git clone https://github.com/codr1/termium.git
cd termium
PUPPETEER_SKIP_DOWNLOAD=true npm ci
npm run build
```

The explicit skip avoids an implicit browser download during dependency installation. Prepare the browser once before launching:

```bash
cd server
npx --no-install puppeteer browsers install chrome
cd ..
./client/termium
```

The browser installation command uses the locally installed Puppeteer version. Its default Chrome download supports Linux x86-64 and macOS Intel/Apple Silicon; [Linux ARM64 needs a separate browser strategy](https://pptr.dev/troubleshooting). Browser system libraries are still required for the current development setup.

The client starts the server automatically. Keep the client binary at `client/termium` so relative server discovery works.

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

The current server uses `/tmp/termium.sock` by default. TCP is unencrypted and unauthenticated; keep manual experiments local. The existing browser launch also disables the Chromium sandbox, which is a public-release blocker.

## Release work

The existing `scripts/build-server-bundle.sh` and tag workflow are being evaluated against the [installation contract](plans/one-command-install.md). They do not currently produce a verified hands-off installation. Follow that plan before publishing a release with one-command setup claims.

The npm lockfile is tracked and CI uses `npm ci`. The tag workflow must pass the Linux/macOS test matrix before packaging. Pinning the separate release generators and validating the runtime bundle remain release work.

[Documentation home](README.md)
