#!/bin/sh

if ! command -v protoc >/dev/null 2>&1; then
  echo >&2 "
Error: 'protoc' is not installed or not found in your PATH.

Please install the Protocol Buffers Compiler (protoc) before proceeding.
You can download it from:

  https://github.com/protocolbuffers/protobuf/releases

Or install it via your package manager.

For Ubuntu/Debian:
  sudo apt update
  sudo apt install -y protobuf-compiler

For MacOS (using Homebrew):
  brew install protobuf

";
  exit 1
else
  echo "protoc is installed: $(protoc --version)"
fi
