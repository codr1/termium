#!/bin/bash
# Build Environment Setup Script for Termium
# Run this script to install all required dependencies

set -e  # Exit on error

echo "======================================="
echo "Termium Build Environment Setup"
echo "======================================="
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if running on Linux
if [[ "$OSTYPE" != "linux-gnu"* ]] && [[ "$OSTYPE" != "darwin"* ]]; then
    echo -e "${RED}This script is for Linux/macOS. For Windows, use WSL2.${NC}"
    exit 1
fi

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

echo "Step 1: Checking prerequisites..."
echo ""

# Check Node.js
if command_exists node; then
    NODE_VERSION=$(node --version)
    echo -e "${GREEN}✓ Node.js installed: $NODE_VERSION${NC}"
else
    echo -e "${YELLOW}! Node.js not found${NC}"
    echo "  Install from: https://nodejs.org/ (recommend v18+)"
    echo "  Or use nvm: curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash"
    exit 1
fi

# Check Go
if command_exists go; then
    GO_VERSION=$(go version)
    echo -e "${GREEN}✓ Go installed: $GO_VERSION${NC}"
else
    echo -e "${YELLOW}! Go not found${NC}"
    echo "  Install from: https://go.dev/dl/ (recommend v1.21+)"
    exit 1
fi

# Check protoc
if command_exists protoc; then
    PROTOC_VERSION=$(protoc --version)
    echo -e "${GREEN}✓ protoc installed: $PROTOC_VERSION${NC}"
else
    echo -e "${YELLOW}! protoc (Protocol Buffers compiler) not found${NC}"
    echo ""
    echo "Installing protoc..."

    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        # Linux
        if command_exists apt-get; then
            echo "Using apt-get..."
            sudo apt-get update
            sudo apt-get install -y protobuf-compiler
        elif command_exists yum; then
            echo "Using yum..."
            sudo yum install -y protobuf-compiler
        else
            echo "Manual installation required. Download from:"
            echo "https://github.com/protocolbuffers/protobuf/releases"
            exit 1
        fi
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        if command_exists brew; then
            echo "Using Homebrew..."
            brew install protobuf
        else
            echo "Please install Homebrew first: https://brew.sh"
            exit 1
        fi
    fi

    # Verify installation
    if command_exists protoc; then
        echo -e "${GREEN}✓ protoc installed successfully${NC}"
    else
        echo -e "${RED}✗ protoc installation failed${NC}"
        exit 1
    fi
fi

echo ""
echo "Step 2: Installing npm dependencies..."
echo ""

npm install
cd server && npm install && cd ..

echo -e "${GREEN}✓ npm dependencies installed${NC}"

echo ""
echo "Step 3: Checking go-sixel dependency..."
echo ""

# Check if go-sixel source exists
if [ -d "third_party/go-sixel" ] && [ -f "third_party/go-sixel/go.mod" ]; then
    echo -e "${GREEN}✓ go-sixel third-party dependency found${NC}"
else
    echo -e "${YELLOW}! go-sixel third-party dependency missing${NC}"
    echo "  Cloning go-sixel fork/custom version..."

    mkdir -p third_party
    cd third_party

    # Clone the go-sixel repository (assuming you have a custom fork)
    # Replace this URL with your actual fork if you have one
    if [ -d "go-sixel" ]; then
        rm -rf go-sixel
    fi

    # Using the original mattn/go-sixel
    git clone https://github.com/mattn/go-sixel.git

    cd ..
    echo -e "${GREEN}✓ go-sixel cloned${NC}"
fi

echo ""
echo "Step 4: Building the project..."
echo ""

npm run build

echo ""
echo -e "${GREEN}=======================================${NC}"
echo -e "${GREEN}✓ Build environment setup complete!${NC}"
echo -e "${GREEN}=======================================${NC}"
echo ""
echo "Next steps:"
echo "  1. Run the client: cd client && ./termium"
echo "  2. The server will auto-start automatically!"
echo ""
echo "For debugging:"
echo "  - Server only: npm run start:server"
echo "  - Client with debug: cd client && ./termium --debug"
echo ""
