#!/bin/bash
# Stress test script to reproduce timing issues (lightweight version)

set -e

echo "=== Stress Testing for Race Conditions ==="

# 1. Run tests multiple times sequentially (safer for CI)
echo "1. Running tests multiple times..."
for i in {1..3}; do
    echo "Run $i"
    go test -race ./... > /dev/null 2>&1
    if [ $? -eq 0 ]; then
        echo "✓ Run $i passed"
    else
        echo "✗ Run $i failed"
        exit 1
    fi
done

# 2. Run pgxpool example tests
echo -e "\n2. Testing pgxpool examples..."
for i in {1..2}; do
    echo "Run $i"
    (cd examples/pgxpool && go test -race ./... > /dev/null 2>&1)
    if [ $? -eq 0 ]; then
        echo "✓ PgxPool run $i passed"
    else
        echo "✗ PgxPool run $i failed"
        exit 1
    fi
done

# 3. Simulate CI environment (separate processes)
echo -e "\n3. Simulating CI environment..."
go test -race ./... > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "✓ Main package passed"
else
    echo "✗ Main package failed"
    exit 1
fi

(cd examples/sqlc && go test -race ./... > /dev/null 2>&1)
if [ $? -eq 0 ]; then
    echo "✓ SQLC examples passed"
else
    echo "✗ SQLC examples failed"
    exit 1
fi

(cd examples/pgxpool && go test -race ./... > /dev/null 2>&1)
if [ $? -eq 0 ]; then
    echo "✓ PgxPool examples passed"
else
    echo "✗ PgxPool examples failed"
    exit 1
fi

echo -e "\nStress test complete!"