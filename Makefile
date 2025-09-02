# ZFSRabbit Makefile

.PHONY: build test test-unit test-integration clean fmt vet lint cover help

# Build the binary
build:
	go build -o bin/zfsrabbit ./

# Run all tests
test: test-unit

# Run unit tests only
test-unit:
	go test -v ./internal/...

# Run tests with coverage
test-cover:
	go test -coverprofile=coverage.out ./internal/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run integration tests (requires external dependencies)
test-integration:
	@echo "Integration tests require ZFS and SSH to be available"
	go test -v ./internal/web/ -tags=integration

# Clean build artifacts
clean:
	rm -f bin/zfsrabbit
	rm -f coverage.out coverage.html

# Format code
fmt:
	gofmt -s -w .

# Vet code
vet:
	go vet ./...

# Run linter (requires staticcheck)
lint:
	staticcheck ./...

# Run all quality checks
check: fmt vet lint test-unit

# Install dependencies
deps:
	go mod download
	go mod tidy

# Build for different platforms
build-all: build-linux build-windows build-darwin

build-linux:
	GOOS=linux GOARCH=amd64 go build -o bin/zfsrabbit-linux-amd64 ./

build-windows:
	GOOS=windows GOARCH=amd64 go build -o bin/zfsrabbit-windows-amd64.exe ./

build-darwin:
	GOOS=darwin GOARCH=amd64 go build -o bin/zfsrabbit-darwin-amd64 ./

# Install systemd service (requires root)
install-service: build
	@if [ "$(USER)" != "root" ]; then echo "Must run as root to install service"; exit 1; fi
	cp bin/zfsrabbit /usr/local/bin/
	cp configs/systemd/zfsrabbit.service /etc/systemd/system/
	systemctl daemon-reload
	systemctl enable zfsrabbit
	@echo "Service installed. Configure /etc/zfsrabbit/config.yaml and run 'systemctl start zfsrabbit'"

# Show help
help:
	@echo "Available targets:"
	@echo "  build          - Build the binary"
	@echo "  test           - Run all tests"
	@echo "  test-unit      - Run unit tests only"
	@echo "  test-cover     - Run tests with coverage report"
	@echo "  test-integration - Run integration tests"
	@echo "  clean          - Clean build artifacts"
	@echo "  fmt            - Format code"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run linter"
	@echo "  check          - Run all quality checks"
	@echo "  deps           - Install dependencies"
	@echo "  build-all      - Build for all platforms"
	@echo "  install-service - Install systemd service (requires root)"
	@echo "  help           - Show this help"