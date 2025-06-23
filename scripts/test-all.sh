#!/bin/bash
# Run all tests across the project including examples and stress tests

set -e

echo "=== Running ALL Tests ==="
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Track failures
FAILED_TESTS=""
TOTAL_FAILURES=0

# Function to run tests in a directory
run_tests() {
    local dir=$1
    local name=$2
    
    echo -e "${YELLOW}Testing $name...${NC}"
    
    if cd "$dir" && go test -v -race ./... 2>&1; then
        echo -e "${GREEN}✓ $name tests passed${NC}"
        echo
        return 0
    else
        echo -e "${RED}✗ $name tests failed${NC}"
        echo
        FAILED_TESTS="$FAILED_TESTS\n  - $name"
        ((TOTAL_FAILURES++))
        return 1
    fi
}

# Function to run a script
run_script() {
    local script=$1
    local name=$2
    
    echo -e "${BLUE}Running $name...${NC}"
    
    if bash "$script"; then
        echo -e "${GREEN}✓ $name completed successfully${NC}"
        echo
        return 0
    else
        echo -e "${RED}✗ $name failed${NC}"
        echo
        FAILED_TESTS="$FAILED_TESTS\n  - $name"
        ((TOTAL_FAILURES++))
        return 1
    fi
}

# Save current directory
ORIGINAL_DIR=$(pwd)

# Run main tests
run_tests "$ORIGINAL_DIR" "Main package"

# Run pgxpool example tests
run_tests "$ORIGINAL_DIR/examples/pgxpool" "PgxPool examples"

# Run sqlc example tests
run_tests "$ORIGINAL_DIR/examples/sqlc" "SQLC examples"

# Run multiple-packages example tests
run_tests "$ORIGINAL_DIR/examples/multiple-packages" "Multiple packages examples"

# Return to original directory
cd "$ORIGINAL_DIR"

# Run stress testing scripts
echo
echo "=== Running Stress Tests ==="
echo

# Check if scripts directory exists
if [ -d "$ORIGINAL_DIR/scripts" ]; then
    # Run stress test
    if [ -f "$ORIGINAL_DIR/scripts/stress-test.sh" ]; then
        run_script "$ORIGINAL_DIR/scripts/stress-test.sh" "Stress test (multiple iterations)"
    fi
    
    # Run concurrent test
    if [ -f "$ORIGINAL_DIR/scripts/concurrent-test.go" ]; then
        echo -e "${BLUE}Running Concurrent package test...${NC}"
        if go run "$ORIGINAL_DIR/scripts/concurrent-test.go"; then
            echo -e "${GREEN}✓ Concurrent package test completed successfully${NC}"
            echo
        else
            echo -e "${RED}✗ Concurrent package test failed${NC}"
            echo
            FAILED_TESTS="$FAILED_TESTS\n  - Concurrent package test"
            ((TOTAL_FAILURES++))
        fi
    fi
    
    # Note: race-detector.sh is available for manual testing but too aggressive for CI
    if [ -f "$ORIGINAL_DIR/scripts/race-detector.sh" ]; then
        echo -e "${YELLOW}Note: race-detector.sh is available for manual race condition testing${NC}"
    fi
    
    # Note: monitor-db.sh is for manual monitoring, not automated testing
    echo -e "${YELLOW}Note: monitor-db.sh is available for manual database monitoring${NC}"
    echo
fi

# Summary
echo "========================================"
if [ $TOTAL_FAILURES -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Test failures detected!${NC}"
    echo -e "${RED}Failed test suites:$FAILED_TESTS${NC}"
    exit 1
fi