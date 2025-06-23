#!/bin/bash
# Monitor database state during tests

echo "=== Database State Monitor ==="

# Function to show current state
show_state() {
    echo -e "\n--- $(date '+%Y-%m-%d %H:%M:%S') ---"
    
    # Show testdbpool_state
    echo "Pool States:"
    psql -h localhost -U postgres -d postgres -c "SELECT pool_id, array_length(available_dbs, 1) as available, array_length(in_use_dbs, 1) as in_use, array_length(failed_dbs, 1) as failed FROM testdbpool_state" 2>/dev/null || echo "No state table"
    
    # Show databases
    echo -e "\nTest Databases:"
    psql -h localhost -U postgres -d postgres -c "SELECT datname FROM pg_database WHERE datname LIKE '%test%' OR datname LIKE '%pool%' ORDER BY datname" 2>/dev/null || true
    
    # Show active connections
    echo -e "\nActive Connections:"
    psql -h localhost -U postgres -d postgres -c "SELECT pid, datname, state, query_start, state_change FROM pg_stat_activity WHERE datname LIKE '%test%' OR datname LIKE '%pool%'" 2>/dev/null || true
}

# Monitor in a loop
while true; do
    show_state
    sleep 2
done