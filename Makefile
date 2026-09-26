.PHONY: build install test vet staticcheck lint clean release release-snapshot test-coverage

# Version from the latest git tag, without the leading "v" (matches GoReleaser's {{.Version}}).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
# Not GOBIN: tools like mise export GOBIN, which would silently override this default.
INSTALL_DIR ?= $(HOME)/.local/bin

# Build the binary
build:
	go build -o paperless-ngx-mcp .

# Install into $(INSTALL_DIR) (default ~/.local/bin) with the version stamped in
install:
	GOBIN=$(INSTALL_DIR) go install -ldflags "-X main.version=$(VERSION)" .

# Run tests
test:
	go test ./...

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# Run go vet
vet:
	go vet ./...

# Run staticcheck
staticcheck:
	staticcheck ./...

# Run vet + staticcheck + test
lint: vet staticcheck test

# Remove build artifacts
clean:
	rm -f paperless-ngx-mcp coverage.out
	rm -rf dist/

# GoReleaser snapshot (local build, no publish)
release-snapshot:
	goreleaser release --snapshot --clean

# GoReleaser release (requires GITHUB_TOKEN, triggered by CI on tags)
release:
	goreleaser release --clean
