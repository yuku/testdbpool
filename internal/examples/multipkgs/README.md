# Multiple Packages Test Example

This example demonstrates cross-process pool sharing where multiple test packages (which run as separate processes) can share the same database pool through the PoolName configuration.

## Current Status

The cross-process pool sharing functionality is working correctly as demonstrated by the `TestCrossProcessPoolSharing` test in the main package. However, these example tests may fail when run in isolation due to:

1. **Table initialization**: Each process needs to ensure the testdbpool management tables exist before operations
2. **Timing issues**: Multiple processes starting simultaneously may encounter race conditions during table creation
3. **Connection management**: Each operation creates its own database connection which may not have access to tables created by other connections

## Usage

For production use, ensure that:
1. The testdbpool tables are created before running tests (they will be created automatically on first use)
2. Use appropriate MaxSize values to handle concurrent access from multiple processes
3. Handle potential race conditions during initial setup

## Running the Tests

To see the cross-process behavior in action, run:
```bash
# From the root directory
go test -v -run TestCrossProcessPoolSharing
```

This test demonstrates that multiple processes can successfully share a pool and respect MaxSize limits.