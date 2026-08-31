BUILD_DIR := third_party/libghostty-vt/build
GHOSTTY_ZIG_OUT := $(CURDIR)/$(BUILD_DIR)/_deps/ghostty-src/zig-out
PKG_CONFIG_PATH := $(GHOSTTY_ZIG_OUT)/share/pkgconfig

STAMP := $(BUILD_DIR)/.ghostty-built

.PHONY: deps build test run clean

# Build libghostty-vt with a baseline CPU target. Ghostty's zig build
# otherwise targets the build machine's native CPU, so release binaries
# use instructions that SIGILL on other CPUs — e.g. inside Docker's Linux
# VM on Apple Silicon (issue #5). GHOSTTY_ZIG_BUILD_FLAGS is ghostty's
# documented pass-through to `zig build`.
ZIG_CPU ?= baseline

$(STAMP):
	cmake -S third_party/libghostty-vt -B $(BUILD_DIR) -DCMAKE_BUILD_TYPE=Release \
		-DGHOSTTY_ZIG_BUILD_FLAGS="-Dcpu=$(ZIG_CPU)"
	cmake --build $(BUILD_DIR)
	@touch $(STAMP)

deps: $(STAMP)

build: $(STAMP)
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go build -o ht ./cmd/ht

test: $(STAMP)
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go test ./...

run: $(STAMP)
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go run ./cmd/ht

clean:
	rm -rf $(BUILD_DIR) ht
