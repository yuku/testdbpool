#!/bin/bash
# Enhanced race condition detection

set -e

echo "=== Enhanced Race Condition Detection ==="

# 1. Run with different GOMAXPROCS values
echo "1. Testing with different GOMAXPROCS..."
for procs in 1 2 4 8; do
    echo "GOMAXPROCS=$procs"
    GOMAXPROCS=$procs go test -v -race -count=3 ./...
done

# 2. Run with stress flags
echo -e "\n2. Running with stress testing..."
go test -v -race -count=10 -cpu=1,2,4 -parallel=4 ./...

# 3. Run with short timeout to catch deadlocks
echo -e "\n3. Testing with short timeouts..."
go test -v -race -timeout=30s ./...

# 4. Memory stress test
echo -e "\n4. Memory stress test..."
GODEBUG=gctrace=1 go test -v -race -run=TestConcurrent ./...