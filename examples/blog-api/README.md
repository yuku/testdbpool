# Blog API Test Example

This example demonstrates how to use `testdbpool` for testing a blog API application with PostgreSQL.

## Overview

The example shows:
- Setting up a test database pool with a realistic schema
- Using transactions for complex operations
- Testing database isolation between tests
- Handling concurrent test execution
- Proper cleanup and reset strategies

## Schema

The blog application has three main tables:
- `users`: Blog authors
- `posts`: Blog posts with publish status
- `comments`: Comments on posts

## Running the Tests

```bash
# Set PostgreSQL connection environment variables
export PGHOST=localhost
export PGUSER=postgres
export PGPASSWORD=postgres

# Run the tests
go test -v
```

## Key Features Demonstrated

### 1. Schema Creation
The `createBlogSchema` function creates a complete schema with:
- Foreign key relationships
- Indexes for performance
- Database triggers for `updated_at` timestamps
- Initial seed data

### 2. Reset Strategy
Uses `ResetByTruncate` to:
- Clear all data from tables (in correct order)
- Re-seed with consistent test data
- Maintain referential integrity

### 3. Test Isolation
Each test gets a clean database with exactly the same initial state, ensuring:
- No test pollution
- Predictable test behavior
- Ability to run tests in any order

### 4. Real-World Patterns
- Transaction usage for atomic operations
- Complex queries with JOINs
- Concurrent access patterns
- Error handling

## Test Cases

1. **TestCreateUser**: Creates a new user and verifies the operation
2. **TestDatabaseIsolation**: Confirms each test starts with clean data
3. **TestCreatePostWithComments**: Uses transactions for related data
4. **TestQueryPostsWithAuthor**: Complex queries with aggregations
5. **TestConcurrentAccess**: Multiple goroutines accessing the pool

## Performance

With `testdbpool`, these tests run significantly faster than traditional approaches because:
- Database setup happens once (template creation)
- Each test gets a pre-initialized database
- Cleanup is efficient (truncate vs drop/create)
- Parallel tests can run concurrently