# CLAUDE.md - Development Instructions for Claude

## Setting Up Build Environment

### Prerequisites

Before building Termium, you need:

1. **Node.js** (v18+)
   - Download: https://nodejs.org/
   - Or use nvm: `curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash`

2. **Go** (v1.21+)
   - Download: https://go.dev/dl/
   - Verify: `go version`

3. **Protocol Buffers Compiler (protoc)**
   - Linux: `sudo apt-get install protobuf-compiler`
   - macOS: `brew install protobuf`
   - Manual: https://github.com/protocolbuffers/protobuf/releases

### Automated Setup

Run the setup script (recommended):
```bash
./setup-build-env.sh
```

This will:
- Check all prerequisites
- Install npm dependencies
- Set up go-sixel third-party dependency
- Build the entire project

### Manual Setup

If the script doesn't work, set up manually:

```bash
# 1. Install npm dependencies
npm install
cd server && npm install && cd ..

# 2. Set up go-sixel (if third_party/go-sixel is empty)
cd third_party
git clone https://github.com/mattn/go-sixel.git
cd ..

# 3. Build everything
npm run build
```

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

### Quick Start (Recommended)

The client now auto-starts the server, so you only need:
```bash
cd client
./termium [options]
```

The server will automatically:
- Start if not already running
- Stop when you exit the client
- Connect via Unix socket (fast, local-only)

### Manual Server Start (For Debugging)

If you want to run the server separately:
```bash
# Terminal 1: Start server manually
npm run start:server

# Terminal 2: Client will detect and use the running server
cd client
./termium [options]
```

### Connection Options

```bash
# Default: Unix socket (auto-start enabled)
./termium

# TCP mode (specify server address)
./termium --tcp               # localhost:50051
./termium --tcp remote:50051  # custom address
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