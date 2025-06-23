#!/bin/bash
# Enhanced race condition detection (lightweight version for CI)

set -e

echo "=== Enhanced Race Condition Detection ==="

# 1. Run with different GOMAXPROCS values (reduced)
echo "1. Testing with different GOMAXPROCS..."
for procs in 2 4; do
    echo "GOMAXPROCS=$procs"
    GOMAXPROCS=$procs go test -race -count=2 ./... > /dev/null 2>&1
    if [ $? -eq 0 ]; then
        echo "✓ GOMAXPROCS=$procs passed"
    else
        echo "✗ GOMAXPROCS=$procs failed"
        exit 1
    fi
done

# 2. Run with moderate stress flags
echo -e "\n2. Running with moderate stress testing..."
go test -race -count=3 -cpu=2,4 ./... > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "✓ Stress test passed"
else
    echo "✗ Stress test failed"
    exit 1
fi

# 3. Run with timeout to catch deadlocks
echo -e "\n3. Testing with timeout..."
go test -race -timeout=60s ./... > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "✓ Timeout test passed"
else
    echo "✗ Timeout test failed"
    exit 1
fi

echo -e "\nAll race detection tests passed!"