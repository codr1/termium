# Development

These instructions are for contributors building the current version. End-user installation is being redesigned around [one command](installation.md).

## Toolchain

Use a supported Node.js LTS release, such as Node 24, and Go 1.23.1 or newer. Install `protoc` using your development environment's package manager. The build also needs the Go protobuf plugins:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
export PATH="$(go env GOPATH)/bin:$PATH"
```

The Go plugin versions above were used for source-build verification. That review used Node 26.5.1 and Go 1.26.5 on Linux AMD64; it did not validate Node 24, the minimum Go version, or native macOS. Node 24 is the proposed release baseline and needs its own checks. Node 18 and 20 are [end-of-life](https://nodejs.org/en/about/previous-releases).

## Build and run

```bash
git clone https://github.com/codr1/termium.git
cd termium
PUPPETEER_SKIP_DOWNLOAD=true npm install
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
npm run build
npm test
go test -race ./...
```

Generated protobuf code is not tracked; a fresh checkout must generate it before Go builds or tests can run. To rebuild just the client after generation, use `npm run build:client`.

The shell integration checks currently use GNU `timeout`; macOS contributors need that tool available for `npm test`. The existing launcher test removes `/tmp/termium.sock`, so stop any development Termium session before running the tests. Both behaviors should be removed from the future portable test suite.

The current tests cover flags, launcher helpers, and parts of rendering. They do not establish end-to-end browser or terminal compatibility. There are no `lint` or `typecheck` npm scripts; the server build runs TypeScript compilation.

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

The npm lockfile is currently ignored, so dependency resolution is not reproducible. Committing a lockfile and pinning release generators are required follow-up work.

[Documentation home](README.md)
