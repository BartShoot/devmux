.PHONY: all build build-daemon build-cli build-cgo test clean install run stop \
        ghostty-fetch ghostty-build ghostty-clean check-zig

# Binary names
DAEMON_BIN := devmuxd
CLI_BIN := devmux

# Output directory
BIN_DIR := bin

# Third party directory
THIRD_PARTY := third_party
GHOSTTY_SRC := $(THIRD_PARTY)/ghostty-src
GHOSTTY_OUT := $(THIRD_PARTY)/ghostty

# Ghostty repository
GHOSTTY_REPO := https://github.com/ghostty-org/ghostty.git
GHOSTTY_TAG := bebca84668947bfc92b9a30ed58712e1c34eee1d

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod

# Build flags
CGO_ENABLED ?= 0
LDFLAGS := -s -w

# CGO flags for libghostty
CGO_CFLAGS := -I$(CURDIR)/$(GHOSTTY_OUT)/include
CGO_LDFLAGS := -L$(CURDIR)/$(GHOSTTY_OUT)/lib -Wl,-rpath,$(CURDIR)/$(GHOSTTY_OUT)/lib -lghostty-vt -lutil -lm

# Default target (no CGO)
all: build

# Build both binaries (no CGO - current behavior)
build: build-daemon build-cli

build-daemon:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(DAEMON_BIN) ./cmd/devmuxd

build-cli:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(CLI_BIN) ./cmd/devmux

# Build with CGO and libghostty terminal emulation
build-cgo: ghostty-build
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
		$(GOBUILD) -tags ghostty -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(DAEMON_BIN) ./cmd/devmuxd
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
		$(GOBUILD) -tags ghostty -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(CLI_BIN) ./cmd/devmux

# Check for Zig compiler
check-zig:
	@which zig > /dev/null 2>&1 || (echo "Error: Zig compiler not found. Install Zig 0.15.x from https://ziglang.org/download/" && exit 1)
	@echo "Zig version: $$(zig version)"

# Fetch ghostty source
ghostty-fetch:
	@mkdir -p $(THIRD_PARTY)
	@if [ ! -d "$(GHOSTTY_SRC)" ]; then \
		echo "Cloning ghostty repository..."; \
		git clone --depth 1 $(GHOSTTY_REPO) $(GHOSTTY_SRC); \
		cd $(GHOSTTY_SRC) && git fetch --depth 1 origin $(GHOSTTY_TAG) && git checkout $(GHOSTTY_TAG); \
	else \
		echo "Ghostty source already exists"; \
	fi

# Build libghostty-vt
ghostty-build: check-zig ghostty-fetch
	@mkdir -p $(GHOSTTY_OUT)/lib $(GHOSTTY_OUT)/include
	@echo "Building libghostty-vt..."
	cd $(GHOSTTY_SRC) && zig build -Demit-lib-vt=true -Doptimize=ReleaseFast
	@echo "Copying library files..."
	cp $(GHOSTTY_SRC)/zig-out/lib/libghostty-vt.* $(GHOSTTY_OUT)/lib/ 2>/dev/null || true
	cp -r $(GHOSTTY_SRC)/zig-out/include/* $(GHOSTTY_OUT)/include/ 2>/dev/null || \
		cp $(GHOSTTY_SRC)/include/*.h $(GHOSTTY_OUT)/include/ 2>/dev/null || true
	@echo "libghostty-vt built successfully"

# Clean ghostty build artifacts
ghostty-clean:
	rm -rf $(GHOSTTY_OUT)
	rm -rf $(GHOSTTY_SRC)/zig-out
	rm -rf $(GHOSTTY_SRC)/zig-cache

# Run tests
test:
	$(GOTEST) -v ./...

# Run tests with race detector
test-race:
	$(GOTEST) -v -race ./...

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR)
	$(GOCMD) clean

# Full clean including third party
distclean: clean ghostty-clean
	rm -rf $(THIRD_PARTY)

# Install dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Install to GOPATH/bin
install: build
	cp $(BIN_DIR)/$(DAEMON_BIN) $(GOPATH)/bin/
	cp $(BIN_DIR)/$(CLI_BIN) $(GOPATH)/bin/

# Development helpers
run: build
	./$(BIN_DIR)/$(DAEMON_BIN) devmux-buddy.yaml

run-verbose: build
	./$(BIN_DIR)/$(DAEMON_BIN) -v devmux-buddy.yaml

ui: build
	./$(BIN_DIR)/$(CLI_BIN) ui

stop:
	./$(BIN_DIR)/$(CLI_BIN) stop 2>/dev/null || pkill -f $(DAEMON_BIN) || true

# Kill any leftover processes from previous runs
kill-all: stop
	-pkill -f "mvn exec:java" 2>/dev/null
	-pkill -f "gradle bootrun" 2>/dev/null
	@echo "Cleaned up processes"

# Build for release (stripped, optimized)
release: CGO_ENABLED=0
release: LDFLAGS=-s -w
release: clean build

# Show help
help:
	@echo "Available targets:"
	@echo ""
	@echo "  Building:"
	@echo "    make build       - Build daemon and CLI (no terminal emulation)"
	@echo "    make build-cgo   - Build with libghostty terminal emulation (requires Zig)"
	@echo ""
	@echo "  libghostty:"
	@echo "    make ghostty-fetch - Clone ghostty repository"
	@echo "    make ghostty-build - Build libghostty-vt (requires Zig 0.15.x)"
	@echo "    make ghostty-clean - Clean ghostty build artifacts"
	@echo "    make check-zig     - Verify Zig installation"
	@echo ""
	@echo "  Testing:"
	@echo "    make test        - Run tests"
	@echo "    make test-race   - Run tests with race detector"
	@echo ""
	@echo "  Running:"
	@echo "    make run         - Build and run daemon with devmux-buddy.yaml"
	@echo "    make run-verbose - Run daemon with verbose output"
	@echo "    make ui          - Build and launch TUI"
	@echo "    make stop        - Stop running daemon"
	@echo "    make kill-all    - Kill daemon and all spawned processes"
	@echo ""
	@echo "  Other:"
	@echo "    make deps        - Download and tidy dependencies"
	@echo "    make install     - Install to GOPATH/bin"
	@echo "    make clean       - Remove build artifacts"
	@echo "    make distclean   - Remove all artifacts including third_party"
	@echo ""
	@echo "Note: Terminal emulation (build-cgo) requires Zig 0.15.x"
	@echo "      Install from: https://ziglang.org/download/"
