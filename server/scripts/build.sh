#!/bin/bash
set -e

# Clean previous builds
npm run clean

# Generate protobuf files
npm run generate

# Build TypeScript
npm run build

echo "Build completed successfully"
