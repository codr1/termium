# CLAUDE.md - Development Instructions for Claude

## Building the Project

**IMPORTANT**: Always use the npm build scripts, even for test builds during development.

### To build the entire project:
```bash
npm run build
```

### To build just the client for testing:
```bash
cd client
go build -o termium
```

The client binary should always be built as `client/termium`.

**DO NOT** build to other locations like `./termium` or `../termium` as this will create confusion and lead to running outdated binaries.

## Running the Application

The client auto-launches the server. Just run:
```bash
cd client
./termium [options]
```

For Ghostty/Kitty terminals:
```bash
./termium --renderer kitty
```

To run the server manually (optional):
```bash
npm run start:server
```

## Testing Performance

When testing with profiling and timings:
```bash
cd client
rm timings  # Clear old timings
./termium -p websafe --timings -cpuprofile=websafe.prof 2>timings
```

## Linting and Type Checking

When code changes are complete, run:
- `npm run lint` (if available)
- `npm run typecheck` (if available)

If these commands don't exist, ask the user for the correct commands and update this file.

## Project Structure

- Server code: `server/`
- Client code: `client/`
- Proto definitions: `proto/`
- Generated code: `server/generated/` and `client/pb/`

## Notes

- The project uses gRPC for client-server communication
- Default connection is Unix domain socket at `/tmp/termium.sock`
- Websafe palette provides best performance for sixel encoding due to caching
- Kitty renderer uses PNG passthrough (no decode/encode on client)
- Client auto-launches server if not already running (searches for server.js relative to binary, then ~/.termium/server/)
- `TERMIUM_SERVER` env var overrides server auto-discovery path
- go-sixel dependency is a fork at github.com/codr1/go-sixel (referenced via replace directive in go.mod)