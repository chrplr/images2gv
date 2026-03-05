#!/bin/bash
set -e

# Determine binary name based on OS
BINARY_NAME="images2gv"
if [[ "$OSTYPE" == "msys" || "$OSTYPE" == "cygwin" ]]; then
    BINARY_NAME="images2gv.exe"
fi

echo "Building images2gv for $(go env GOOS)/$(go env GOARCH)..."

# Build the project
go build -v -o "$BINARY_NAME" ./cmd/images2gv/main.go

echo "Build successful: ./$BINARY_NAME"
