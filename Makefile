# Alice Proto-First Build System
.PHONY: all proto proto-gen proto-lint proto-clean go-build test clean help

# Default target
all: proto go-build

# Proto-related targets
proto: proto-lint proto-gen

# Generate code from proto files
proto-gen:
	@echo "🔄 Generating code from proto files..."
	cd proto && buf generate
	@echo "✅ Proto code generation complete"

# Lint proto files
proto-lint:
	@echo "🔍 Linting proto files..."
	cd proto && buf lint
	@echo "✅ Proto linting complete"

# Format proto files
proto-format:
	@echo "📝 Formatting proto files..."
	cd proto && buf format --write
	@echo "✅ Proto formatting complete"

# Check for breaking changes
proto-breaking:
	@echo "🔒 Checking for breaking changes..."
	cd proto && buf breaking --against '.git#branch=main'
	@echo "✅ No breaking changes detected"

# Clean generated files
proto-clean:
	@echo "🧹 Cleaning generated proto files..."
	rm -rf gen/
	@echo "✅ Proto cleanup complete"

# Generate dependencies file for Go modules
go-mod-tidy:
	go mod tidy

# Build Go application
go-build:
	@echo "🏗️  Building Go application..."
	go build -o alice ./cmd/alice
	@echo "✅ Go build complete"

# Run tests
test:
	@echo "🧪 Running tests..."
	go test -v ./...
	@echo "✅ Tests complete"

# Run tests with coverage
test-coverage:
	@echo "🧪 Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report generated: coverage.html"

# Install buf if not present
install-buf:
	@echo "📦 Installing buf..."
	@if ! command -v buf &> /dev/null; then \
		echo "Installing buf via go install..."; \
		go install github.com/bufbuild/buf/cmd/buf@latest; \
	else \
		echo "buf already installed: $$(buf --version)"; \
	fi
	@echo "✅ buf installation complete"

# Install protoc if not present
install-protoc:
	@echo "📦 Installing protoc..."
	@if ! command -v protoc &> /dev/null; then \
		echo "Please install protoc manually:"; \
		echo "  macOS: brew install protobuf"; \
		echo "  Ubuntu: apt-get install protobuf-compiler"; \
		echo "  Windows: https://protobuf.dev/downloads/"; \
		exit 1; \
	else \
		echo "protoc already installed: $$(protoc --version)"; \
	fi

# Setup development environment
setup: install-buf install-protoc
	@echo "🔧 Setting up development environment..."
	go mod download
	mkdir -p gen/go gen/ts gen/openapi
	@echo "✅ Development environment setup complete"

# Docker build
docker-build:
	@echo "🐳 Building Docker image..."
	docker build -t alice:proto-latest .
	@echo "✅ Docker build complete"

# Run development server with hot reload
dev:
	@echo "🚀 Starting development server..."
	go run ./cmd/alice &
	fswatch -o . | xargs -n1 -I{} sh -c 'pkill -f "go run" && go run ./cmd/alice &'

# Production build
prod-build: proto go-build
	@echo "🎯 Production build complete"

# Clean all generated files and build artifacts
clean: proto-clean
	@echo "🧹 Cleaning build artifacts..."
	rm -f alice coverage.out coverage.html
	go clean
	@echo "✅ Cleanup complete"

# Show available targets
help:
	@echo "Alice Proto-First Build System"
	@echo ""
	@echo "Available targets:"
	@echo "  all           - Build everything (proto + go)"
	@echo "  proto         - Generate code from proto files"
	@echo "  proto-gen     - Generate Go/TypeScript code"
	@echo "  proto-lint    - Lint proto files"
	@echo "  proto-format  - Format proto files"
	@echo "  proto-breaking - Check for breaking changes"
	@echo "  proto-clean   - Clean generated proto files"
	@echo "  go-build      - Build Go application"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  setup         - Setup development environment"
	@echo "  dev           - Start development server"
	@echo "  prod-build    - Production build"
	@echo "  clean         - Clean all generated files"
	@echo "  help          - Show this help message"