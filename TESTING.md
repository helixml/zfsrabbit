# ZFSRabbit Testing Guide

This document describes the testing setup and procedures for ZFSRabbit.

## Test Structure

The test suite includes:
- **Unit tests**: Test individual components with mocked dependencies
- **Integration tests**: Test HTTP handlers and component interactions
- **Mock framework**: Comprehensive mocks for external dependencies

## Test Files

- `test/mocks/` - Mock implementations for all external dependencies
  - `command.go` - Mock command executor for testing ZFS operations
  - `ssh.go` - Mock SSH transport for testing remote operations
  - `alerter.go` - Mock alerting system
- `internal/*/test.go` - Unit tests for each internal package
- `internal/web/server_test.go` - Integration tests for web API

## Running Tests

### Using Make

```bash
# Run all unit tests
make test

# Run unit tests only
make test-unit

# Run tests with coverage report
make test-cover

# Run all quality checks (format, vet, lint, test)
make check
```

### Using the Test Script

```bash
# Run comprehensive test suite
./scripts/test.sh
```

### Using Go Commands

```bash
# Run all tests
go test ./internal/...

# Run tests with verbose output
go test -v ./internal/...

# Run tests with coverage
go test -coverprofile=coverage.out ./internal/...
go tool cover -html=coverage.out -o coverage.html

# Run specific package tests
go test ./internal/zfs/
go test ./internal/transport/
```

## Test Configuration

- `configs/test/config.yaml` - Test configuration file
- Environment variables used in tests:
  - `ZFSRABBIT_ADMIN_PASSWORD` - Admin password for web interface tests

## Mock Framework

The test suite uses comprehensive mocks for all external dependencies:

### ZFS Manager Mock
- Simulates ZFS commands without requiring actual ZFS
- Tracks command calls for verification
- Can simulate errors and various system states

### SSH Transport Mock  
- Simulates remote SSH operations
- Mocks snapshot transfers and remote command execution
- Supports testing cross-dataset scenarios

### Alert System Mock
- Captures alert calls for verification
- Supports both email and Slack alert testing
- Tracks alert history and timing

## Coverage Goals

- Target: >80% code coverage
- Focus on critical paths: snapshot creation, restore operations, monitoring
- Mock external dependencies to achieve consistent test results

## CI/CD Integration

GitHub Actions workflow (`.github/workflows/test.yml`):
- Runs on push/PR to main branches
- Executes full test suite
- Generates coverage reports
- Performs code quality checks

## Integration Test Limitations

Integration tests have some limitations due to external dependencies:
- ZFS commands require actual ZFS installation
- SSH operations require network connectivity
- Some tests are skipped in environments without these dependencies

## Running Tests in Development

During development:

1. Run unit tests frequently: `make test-unit`
2. Check coverage before commits: `make test-cover`
3. Run quality checks: `make check`
4. Use the full test script before major changes: `./scripts/test.sh`

## Troubleshooting

### Common Issues

1. **"zfs command not found"** - ZFS not installed (expected in test environments)
2. **Coverage variations** - Some code paths depend on system state
3. **Integration test failures** - Often due to missing external dependencies

### Solutions

- Unit tests should work without external dependencies
- Use mocks for testing external interactions
- Integration test failures in CI are often acceptable if unit tests pass