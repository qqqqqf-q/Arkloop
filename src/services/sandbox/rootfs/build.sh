#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd)"

TARGET="${TARGET:-python3.12}"
OUTPUT_DIR="${OUTPUT_DIR:-$SCRIPT_DIR/output}"

case "$TARGET" in
    python3.12)
        DOCKERFILE="$SCRIPT_DIR/Dockerfile.python3.12"
        OUTPUT_FILE="python3.12.ext4"
        ;;
    chromium)
        DOCKERFILE="$SCRIPT_DIR/Dockerfile.chromium"
        OUTPUT_FILE="chromium.ext4"
        ;;
    *)
        printf 'unsupported rootfs target: %s\n' "$TARGET" >&2
        exit 1
        ;;
esac

echo "building rootfs: $OUTPUT_FILE"
echo "target: $TARGET"
echo "context: $PROJECT_ROOT"

mkdir -p "$OUTPUT_DIR"

docker buildx build \
    --platform "${PLATFORM:-linux/amd64}" \
    --file "$DOCKERFILE" \
    --output "type=local,dest=$OUTPUT_DIR" \
    "$PROJECT_ROOT"

echo "output: $OUTPUT_DIR/$OUTPUT_FILE"
ls -lh "$OUTPUT_DIR/$OUTPUT_FILE"

if [[ -n "${DEPLOY_HOST:-}" ]]; then
    DEPLOY_PATH="${DEPLOY_PATH:-/opt/sandbox/rootfs}"
    echo "deploying to $DEPLOY_HOST:$DEPLOY_PATH"
    ssh "$DEPLOY_HOST" "mkdir -p $DEPLOY_PATH"
    scp "$OUTPUT_DIR/$OUTPUT_FILE" "$DEPLOY_HOST:$DEPLOY_PATH/$OUTPUT_FILE"
    echo "deployed"
fi
