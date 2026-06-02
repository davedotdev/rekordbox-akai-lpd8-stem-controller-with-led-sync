#!/bin/bash

# Build script for rb-lpd8-led-bridge
#
# Usage:
#   ./build.sh <version>            build for the current platform
#                                   (macOS builds both arm64 and amd64)
#   ./build.sh <version> windows    cross-compile a Windows amd64 .exe
#                                   (needs mingw-w64: brew install mingw-w64)
#
# rtmidi uses CGO, so each target needs a matching C/C++ toolchain.

set -e

VERSION=${1:-"dev"}
TARGET="${2:-}"
OUTPUT_DIR="releases"
APP_NAME="rb-lpd8-led-bridge"

echo "Building $APP_NAME version $VERSION..."
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Detect current platform
CURRENT_OS=$(uname -s | tr '[:upper:]' '[:lower:]')
CURRENT_ARCH=$(uname -m)

# Map architecture names
case "$CURRENT_ARCH" in
    x86_64) CURRENT_ARCH="amd64" ;;
    arm64|aarch64) CURRENT_ARCH="arm64" ;;
    i386|i686) CURRENT_ARCH="386" ;;
esac

case "$CURRENT_OS" in
    darwin) CURRENT_OS="darwin" ;;
    linux) CURRENT_OS="linux" ;;
    mingw*|msys*|cygwin*) CURRENT_OS="windows" ;;
esac

echo "Detected platform: $CURRENT_OS/$CURRENT_ARCH"
echo ""

# Windows cross-build (opt-in): ./build.sh <version> windows  — needs mingw-w64.
if [ "$TARGET" = "windows" ] && [ "$CURRENT_OS" != "windows" ]; then
    MINGW_CC="x86_64-w64-mingw32-gcc"
    MINGW_CXX="x86_64-w64-mingw32-g++"
    if ! command -v "$MINGW_CXX" >/dev/null 2>&1; then
        echo "Windows cross-build needs mingw-w64 ($MINGW_CXX not found)." >&2
        echo "  macOS:  brew install mingw-w64" >&2
        echo "  Debian: apt install gcc-mingw-w64" >&2
        echo "Or build on a Windows machine: run ./build.sh $VERSION there." >&2
        exit 1
    fi
    echo "Cross-building for windows/amd64 with mingw-w64..."
    CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC="$MINGW_CC" CXX="$MINGW_CXX" \
        go build -ldflags "-X main.Version=$VERSION" -o "$OUTPUT_DIR/${APP_NAME}-windows-amd64.exe" .
    echo ""
    echo "Build complete:"
    ls -la "$OUTPUT_DIR/${APP_NAME}-windows-amd64.exe"
    echo ""
    echo "Note: Windows binaries are not yet code-signed (signing/SmartScreen pending)."
    echo "Config is platform-independent — reuse releases/config.json or the bundled config.json."
    exit 0
fi

# Build for current platform
if [ "$CURRENT_OS" = "windows" ]; then
    EXT=".exe"
else
    EXT=""
fi

# On macOS, build for both arm64 and amd64
if [ "$CURRENT_OS" = "darwin" ]; then
    echo "Building for darwin/arm64..."
    CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CGO_CFLAGS="-arch arm64" CGO_LDFLAGS="-arch arm64" \
        go build -ldflags "-X main.Version=$VERSION" -o "$OUTPUT_DIR/${APP_NAME}-darwin-arm64" .

    echo "Building for darwin/amd64..."
    CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CGO_CFLAGS="-arch x86_64" CGO_LDFLAGS="-arch x86_64" \
        go build -ldflags "-X main.Version=$VERSION" -o "$OUTPUT_DIR/${APP_NAME}-darwin-amd64" .
else
    echo "Building for $CURRENT_OS/$CURRENT_ARCH..."
    go build -ldflags "-X main.Version=$VERSION" -o "$OUTPUT_DIR/${APP_NAME}-${CURRENT_OS}-${CURRENT_ARCH}${EXT}" .
fi

# Generate default config (use current architecture binary)
echo "Generating default config..."
if [ "$CURRENT_OS" = "darwin" ]; then
    "$OUTPUT_DIR/${APP_NAME}-darwin-${CURRENT_ARCH}" -genconfig "$OUTPUT_DIR/config.json"
else
    "$OUTPUT_DIR/${APP_NAME}-${CURRENT_OS}-${CURRENT_ARCH}${EXT}" -genconfig "$OUTPUT_DIR/config.json"
fi

echo ""
echo "Build complete! Files in $OUTPUT_DIR/:"
ls -la "$OUTPUT_DIR/"

echo ""
echo "macOS builds both arm64 and amd64 automatically."
echo "For Windows: './build.sh $VERSION windows' (cross-compile, needs mingw-w64),"
echo "or run this script on a Windows machine."
