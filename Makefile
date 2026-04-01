.PHONY: all build build-daemon build-cli test clean install run stop \
        ghostty-fetch ghostty-build ghostty-clean check-zig build-test apps

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
GOMOD := $(GOCMD) mod

# Build flags
LDFLAGS := -s -w

# CGO flags for libghostty
CGO_CFLAGS := -I$(CURDIR)/$(GHOSTTY_OUT)/include
CGO_LDFLAGS := -L$(CURDIR)/$(GHOSTTY_OUT)/lib -Wl,-rpath,$(CURDIR)/$(GHOSTTY_OUT)/lib -lghostty-vt -lutil -lm

# Default target
all: build

# Build everything
build: ghostty-build build-daemon build-cli build-test

build-daemon:
	@mkdir -p $(BIN_DIR)
	@echo "Building daemon with libghostty..."
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
		$(GOBUILD) -tags ghostty -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(DAEMON_BIN) ./cmd/devmuxd

build-cli:
	@mkdir -p $(BIN_DIR)
	@echo "Building CLI (pure Go)..."
	CGO_ENABLED=0 $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(CLI_BIN) ./cmd/devmux

# Build test applications
build-test:
	@mkdir -p $(BIN_DIR)
	@echo "Building test apps..."
	@for app in http-app log-app tcp-app; do \
		echo "  Building $$app..."; \
		$(GOBUILD) -o $(BIN_DIR)/$$app ./test-apps/$$app; \
	done

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
	@if [ ! -f "$(GHOSTTY_OUT)/lib/libghostty-vt.so" ] && [ ! -f "$(GHOSTTY_OUT)/lib/ghostty-vt.lib" ]; then \
		echo "Building libghostty-vt..."; \
		cd $(GHOSTTY_SRC) && zig build -Demit-lib-vt=true -Doptimize=ReleaseFast; \
		echo "Copying library files..."; \
		cp $(GHOSTTY_SRC)/zig-out/lib/libghostty-vt.* $(GHOSTTY_OUT)/lib/ 2>/dev/null || true; \
		cp -r $(GHOSTTY_SRC)/zig-out/include/* $(GHOSTTY_OUT)/include/ 2>/dev/null || \
			cp $(GHOSTTY_SRC)/include/*.h $(GHOSTTY_OUT)/include/ 2>/dev/null || true; \
	else \
		echo "libghostty-vt already built"; \
	fi

# Clean ghostty build artifacts
ghostty-clean:
	rm -rf $(GHOSTTY_OUT)
	rm -rf $(GHOSTTY_SRC)/zig-out
	rm -rf $(GHOSTTY_SRC)/zig-cache

# Run tests
test:
	$(GOTEST) -v ./...

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR)
	$(GOCMD) clean

# Full clean including third party
distclean: clean ghostty-clean
	rm -rf $(THIRD_PARTY)

# Development helpers
run: build
	./$(BIN_DIR)/$(DAEMON_BIN) devmux.yaml

ui: build-cli
	./$(BIN_DIR)/$(CLI_BIN) ui

stop:
	./$(BIN_DIR)/$(CLI_BIN) stop 2>/dev/null || pkill -f $(DAEMON_BIN) || true

# Show help
help:
	@echo "Available targets:"
	@echo ""
	@echo "  make build       - Build daemon (with CGO), CLI, and test apps"
	@echo "  make ghostty-build - Build libghostty-vt (requires Zig 0.15.x)"
	@echo "  make test        - Run tests"
	@echo "  make run         - Build and run daemon"
	@echo "  make ui          - Build and launch TUI"
	@echo "  make clean       - Remove build artifacts"
	@echo ""
	@echo "Note: Full build requires Zig 0.15.x for terminal emulation."
