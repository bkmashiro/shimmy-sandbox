#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist"
WORK_DIR="$DIST_DIR/layer-build"
LAYER_ZIP="$DIST_DIR/lambda-layer.zip"

DYNAMORIO_VERSION="10.0.0"
DYNAMORIO_TARBALL="DynamoRIO-Linux-10.0.0.tar.gz"
DYNAMORIO_URL="https://github.com/DynamoRIO/dynamorio/releases/download/release_10.0.0/${DYNAMORIO_TARBALL}"
DYNAMORIO_EXTRACTED="DynamoRIO-Linux-10.0.0"

PROTOTYPES_DIR="$(cd "$REPO_ROOT/.." && pwd)/shimmy-sandbox-prototypes/proto-d-dynamorio"

# ---------------------------------------------------------------------------
# Step 1: Build shimmy-sandbox static linux/amd64 binary
# ---------------------------------------------------------------------------
echo "==> Building shimmy-sandbox (linux/amd64, static)..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags="-s -w" \
  -o "$DIST_DIR/shimmy-sandbox-linux-amd64" \
  "$REPO_ROOT/cmd/shimmy-sandbox"
echo "    Built: $DIST_DIR/shimmy-sandbox-linux-amd64"

# ---------------------------------------------------------------------------
# Step 2: Download DynamoRIO
# ---------------------------------------------------------------------------
mkdir -p "$WORK_DIR"
TARBALL_PATH="$WORK_DIR/$DYNAMORIO_TARBALL"

if [[ -f "$TARBALL_PATH" ]]; then
  echo "==> DynamoRIO tarball already cached at $TARBALL_PATH, skipping download."
else
  echo "==> Downloading DynamoRIO ${DYNAMORIO_VERSION}..."
  curl -fSL --progress-bar -o "$TARBALL_PATH" "$DYNAMORIO_URL"
fi

# ---------------------------------------------------------------------------
# Step 3: Extract drrun + libdynamorio.so
# ---------------------------------------------------------------------------
echo "==> Extracting DynamoRIO binaries..."
EXTRACT_DIR="$WORK_DIR/dynamorio-extracted"
mkdir -p "$EXTRACT_DIR"

tar -xzf "$TARBALL_PATH" -C "$EXTRACT_DIR" \
  "${DYNAMORIO_EXTRACTED}/bin64/drrun" \
  "${DYNAMORIO_EXTRACTED}/lib64/release/libdynamorio.so"

DRRUN_SRC="$EXTRACT_DIR/${DYNAMORIO_EXTRACTED}/bin64/drrun"
LIBDRIO_SRC="$EXTRACT_DIR/${DYNAMORIO_EXTRACTED}/lib64/release/libdynamorio.so"

echo "    drrun:          $DRRUN_SRC"
echo "    libdynamorio.so: $LIBDRIO_SRC"

# ---------------------------------------------------------------------------
# Step 4: syscall_filter.so — build from prototypes repo or warn
# ---------------------------------------------------------------------------
FILTER_SO_PATH=""

if [[ -d "$PROTOTYPES_DIR" && -f "$PROTOTYPES_DIR/CMakeLists.txt" ]]; then
  echo "==> Found shimmy-sandbox-prototypes at $PROTOTYPES_DIR, building syscall_filter.so..."
  BUILD_DIR="$WORK_DIR/syscall_filter_build"
  mkdir -p "$BUILD_DIR"

  if ! command -v cmake &>/dev/null; then
    echo "    WARNING: cmake not found, cannot build syscall_filter.so"
    echo "    Layer will run without syscall filtering."
    FILTER_SO_PATH=""
  else
    (
      cd "$BUILD_DIR"
      cmake "$PROTOTYPES_DIR" -DCMAKE_BUILD_TYPE=Release 2>&1 | sed 's/^/    /'
      cmake --build . --target syscall_filter 2>&1 | sed 's/^/    /'
    )
    if [[ -f "$BUILD_DIR/syscall_filter.so" ]]; then
      FILTER_SO_PATH="$BUILD_DIR/syscall_filter.so"
      echo "    Built: $FILTER_SO_PATH"
    else
      echo "    WARNING: cmake build did not produce syscall_filter.so"
      echo "    Layer will run without syscall filtering."
    fi
  fi
else
  echo "==> shimmy-sandbox-prototypes not found at $PROTOTYPES_DIR"
  echo "    WARNING: syscall_filter.so not found, layer will run without syscall filtering."
fi

# ---------------------------------------------------------------------------
# Step 5: Assemble layer zip
# ---------------------------------------------------------------------------
echo "==> Assembling lambda-layer.zip..."
STAGE_DIR="$WORK_DIR/stage"
rm -rf "$STAGE_DIR"

mkdir -p \
  "$STAGE_DIR/opt/shimmy-sandbox/bin" \
  "$STAGE_DIR/opt/dynamorio/bin64" \
  "$STAGE_DIR/opt/dynamorio/lib64/release" \
  "$STAGE_DIR/opt/sandbox"

cp "$DIST_DIR/shimmy-sandbox-linux-amd64"  "$STAGE_DIR/opt/shimmy-sandbox/bin/shimmy-sandbox"
chmod +x "$STAGE_DIR/opt/shimmy-sandbox/bin/shimmy-sandbox"

cp "$DRRUN_SRC"   "$STAGE_DIR/opt/dynamorio/bin64/drrun"
chmod +x "$STAGE_DIR/opt/dynamorio/bin64/drrun"

cp "$LIBDRIO_SRC" "$STAGE_DIR/opt/dynamorio/lib64/release/libdynamorio.so"

if [[ -n "$FILTER_SO_PATH" ]]; then
  cp "$FILTER_SO_PATH" "$STAGE_DIR/opt/sandbox/syscall_filter.so"
else
  # Leave opt/sandbox/ empty — Lambda will run with rlimits-only backend
  echo "    (opt/sandbox/syscall_filter.so omitted — rlimits-only mode)"
fi

mkdir -p "$DIST_DIR"
(cd "$STAGE_DIR" && zip -r9 "$LAYER_ZIP" opt/)

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
echo ""
echo "==> Layer built successfully:"
du -sh "$LAYER_ZIP"
echo ""
echo "Deploy instructions:"
echo ""
echo "  # Publish the layer:"
echo "  aws lambda publish-layer-version \\"
echo "    --layer-name shimmy-sandbox \\"
echo "    --zip-file fileb://$LAYER_ZIP \\"
echo "    --compatible-runtimes provided.al2023 \\"
echo "    --compatible-architectures x86_64"
echo ""
echo "  # Attach to your Lambda function:"
echo "  aws lambda update-function-configuration \\"
echo "    --function-name <your-function> \\"
echo "    --layers <LayerVersionArn from above>"
