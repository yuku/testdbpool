#!/bin/bash
set -e

echo "Running multiple-packages example tests..."
echo "========================================"

# Change to the example directory
cd "$(dirname "$0")"

# Run tests for all packages
echo "Running tests across multiple packages..."
go test -v ./... -count=1

echo ""
echo "Tests completed successfully!"