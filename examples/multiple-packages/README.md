# Multiple Packages Example

This example demonstrates using `testdbpool` directly (without the pgxpool wrapper) across multiple test packages, similar to real-world scenarios where different packages test different parts of an application.

## Purpose

This example helps isolate whether test failures are due to:
- Issues in the core `testdbpool` library
- Issues in the `testdbpool/pgxpool` wrapper
- Issues with test setup or configuration

## Structure

- `shared/` - Common setup code and pool initialization
- `package1/` - User-related tests
- `package2/` - Product-related tests  
- `package3/` - Order-related tests

## Features Demonstrated

1. **Pool Sharing**: All packages share the same database pool
2. **Test Isolation**: Each test gets a clean database state
3. **Static Table Preservation**: Categories table is excluded from truncation
4. **Transaction Support**: Tests demonstrate transaction usage
5. **Concurrent Operations**: Tests run across multiple packages

## Running Tests

```bash
./run-tests.sh
```

Or manually:

```bash
go test -v ./... -count=1
```

## Key Differences from pgxpool Example

- Uses `database/sql` directly with lib/pq driver
- No pgx-specific features (batch queries, COPY, etc.)
- Simpler setup focused on core testdbpool functionality
- Helps identify if issues are in core library vs wrapper