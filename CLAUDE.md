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

### Server:
```bash
npm run start:server
```

### Client:
```bash
cd client
./termium [options]
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