#!/bin/bash
set -e

echo "Cleaning previous builds..."
npm run clean

echo "Installing dependencies..."
npm run install:all

echo "Building project..."
npm run build

echo "Build completed successfully!"
echo ""
echo "To start the application:"
echo "Terminal 1: npm run start:server"
echo "Terminal 2: npm run start:client"
