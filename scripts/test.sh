#!/bin/bash

# Test runner script for ZFSRabbit

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Running ZFSRabbit Test Suite${NC}"
echo "==============================="

# Set test environment variables
export ZFSRABBIT_ADMIN_PASSWORD="testpassword"

# Change to project directory
cd "$PROJECT_ROOT"

# Clean up any previous test artifacts
echo -e "${YELLOW}Cleaning up previous test artifacts...${NC}"
rm -f coverage.out coverage.html

# Run unit tests
echo -e "${YELLOW}Running unit tests...${NC}"
if go test -v ./internal/...; then
    echo -e "${GREEN}✓ Unit tests passed${NC}"
else
    echo -e "${RED}✗ Unit tests failed${NC}"
    exit 1
fi

# Run tests with coverage
echo -e "${YELLOW}Running tests with coverage...${NC}"
if go test -coverprofile=coverage.out ./internal/...; then
    echo -e "${GREEN}✓ Coverage tests passed${NC}"
    
    # Generate HTML coverage report
    go tool cover -html=coverage.out -o coverage.html
    echo -e "${GREEN}Coverage report generated: coverage.html${NC}"
    
    # Show coverage summary
    coverage=$(go tool cover -func=coverage.out | grep total: | awk '{print $3}')
    echo -e "${GREEN}Total coverage: $coverage${NC}"
else
    echo -e "${RED}✗ Coverage tests failed${NC}"
    exit 1
fi

# Run go vet
echo -e "${YELLOW}Running go vet...${NC}"
if go vet ./...; then
    echo -e "${GREEN}✓ go vet passed${NC}"
else
    echo -e "${RED}✗ go vet failed${NC}"
    exit 1
fi

# Check formatting
echo -e "${YELLOW}Checking code formatting...${NC}"
if [ "$(gofmt -s -l . | wc -l)" -gt 0 ]; then
    echo -e "${RED}✗ Code formatting issues found:${NC}"
    gofmt -s -l .
    exit 1
else
    echo -e "${GREEN}✓ Code formatting is correct${NC}"
fi

# Try to run staticcheck if available
echo -e "${YELLOW}Running staticcheck (if available)...${NC}"
if command -v staticcheck &> /dev/null; then
    if staticcheck ./...; then
        echo -e "${GREEN}✓ staticcheck passed${NC}"
    else
        echo -e "${RED}✗ staticcheck failed${NC}"
        exit 1
    fi
else
    echo -e "${YELLOW}⚠ staticcheck not available, skipping${NC}"
fi

echo ""
echo -e "${GREEN}🎉 All tests passed!${NC}"
echo "==============================="

# Show test summary
echo "Test Summary:"
echo "- Unit tests: ✓"
echo "- Coverage: $coverage"
echo "- go vet: ✓"
echo "- Code formatting: ✓"
if command -v staticcheck &> /dev/null; then
    echo "- staticcheck: ✓"
fi

echo ""
echo "Coverage report available at: coverage.html"