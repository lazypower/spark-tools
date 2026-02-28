set dotenv-load := false

bin := "$HOME/.local/bin"
dist-arm64 := "dist/linux-arm64"
dist-amd64 := "dist/linux-amd64"
go := "devbox run -- go"

# List available recipes
default:
    @just --list

# Build all binaries to ~/.local/bin
build: build-hfetch build-llm-run build-llm-bench

# Build hfetch
build-hfetch:
    {{go}} build -o {{bin}}/hfetch ./cmd/hfetch

# Build llm-run
build-llm-run:
    {{go}} build -o {{bin}}/llm-run ./cmd/llm-run

# Build llm-bench
build-llm-bench:
    {{go}} build -o {{bin}}/llm-bench ./cmd/llm-bench

# Cross-compile all binaries for Linux ARM64 (e.g. DGX Spark)
build-linux-arm64:
    mkdir -p {{dist-arm64}}
    GOOS=linux GOARCH=arm64 CGO_ENABLED=0 {{go}} build -o {{dist-arm64}}/hfetch ./cmd/hfetch
    GOOS=linux GOARCH=arm64 CGO_ENABLED=0 {{go}} build -o {{dist-arm64}}/llm-run ./cmd/llm-run
    GOOS=linux GOARCH=arm64 CGO_ENABLED=0 {{go}} build -o {{dist-arm64}}/llm-bench ./cmd/llm-bench
    @echo "Built to {{dist-arm64}}/"

# Cross-compile all binaries for Linux AMD64
build-linux-amd64:
    mkdir -p {{dist-amd64}}
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 {{go}} build -o {{dist-amd64}}/hfetch ./cmd/hfetch
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 {{go}} build -o {{dist-amd64}}/llm-run ./cmd/llm-run
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 {{go}} build -o {{dist-amd64}}/llm-bench ./cmd/llm-bench
    @echo "Built to {{dist-amd64}}/"

# Run all tests
test:
    {{go}} test ./...

# Run tests with coverage summary
test-cover:
    {{go}} test -cover ./...

# Run tests for a specific package (e.g. just test-pkg llmrun/engine)
test-pkg pkg:
    {{go}} test -v -cover ./pkg/{{pkg}}/...

# Run go vet
vet:
    {{go}} vet ./...

# Build + vet + test
check: vet test build

# Clean built binaries
clean:
    rm -f {{bin}}/hfetch {{bin}}/llm-run {{bin}}/llm-bench
    rm -rf dist/

# Install (alias for build)
install: build
    @echo "Installed to {{bin}}"
