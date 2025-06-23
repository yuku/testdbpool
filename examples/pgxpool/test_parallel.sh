#!/bin/bash

# Test script to run all packages in parallel and verify pool sharing

echo "Running all packages in parallel with go test ./..."
echo "This tests that testdbpool correctly manages the pool across multiple packages"
echo ""

# Set environment variables
export DB_HOST="${DB_HOST:-db}"
export DB_USER="${DB_USER:-postgres}"
export DB_PASSWORD="${DB_PASSWORD:-postgres}"

# Run tests with verbose output
echo "Starting parallel test execution..."
time go test -v ./... -parallel 4

# Check the exit code
if [ $? -eq 0 ]; then
    echo ""
    echo "✅ All tests passed! The pool was successfully shared across packages."
else
    echo ""
    echo "❌ Some tests failed."
    exit 1
fi