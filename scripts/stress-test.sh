#!/bin/bash
# Stress test script to reproduce timing issues

set -e

echo "=== Stress Testing for Race Conditions ==="

# 1. Run tests multiple times in parallel
echo "1. Running tests in parallel multiple times..."
for i in {1..10}; do
    echo "Run $i"
    go test -v -race ./... &
done
wait

# 2. Run pgxpool example tests with clean state each time
echo -e "\n2. Testing pgxpool examples with fresh state..."
for i in {1..5}; do
    echo "Run $i"
    # Clean up any existing test databases
    psql -h localhost -U postgres -d postgres -c "DROP DATABASE IF EXISTS pgxpool_multi_pkg_template" 2>/dev/null || true
    psql -h localhost -U postgres -d postgres -c "DELETE FROM testdbpool_state WHERE pool_id = 'pgxpool_multi_pkg'" 2>/dev/null || true
    
    # Run tests
    (cd examples/pgxpool && go test -v -race ./...)
done

# 3. Simulate CI environment (separate processes)
echo -e "\n3. Simulating CI environment..."
go test -v -race ./...
cd examples/sqlc && go test -v -race ./...
cd ../pgxpool && go test -v -race ./...

echo -e "\nStress test complete!"