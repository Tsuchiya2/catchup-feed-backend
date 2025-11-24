#!/bin/bash
# ============================================================
# マルチアーキテクチャ Docker イメージビルドスクリプト
# ============================================================
# AMD64 と ARM64 の両方に対応したイメージを作成します
#
# 使用方法:
#   ./scripts/build-multiarch.sh [OPTIONS]
#
# オプション:
#   --push        Docker Hub / レジストリにプッシュ
#   --tag TAG     イメージタグ指定（デフォルト: latest）
#   --registry    レジストリURL（デフォルト: なし）
# ============================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# カラー出力
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# デフォルト設定
PUSH=false
TAG="latest"
REGISTRY=""
IMAGE_NAME="catchup-feed"

# 引数パース
while [[ $# -gt 0 ]]; do
    case $1 in
        --push)
            PUSH=true
            shift
            ;;
        --tag)
            TAG="$2"
            shift 2
            ;;
        --registry)
            REGISTRY="$2/"
            shift 2
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

FULL_IMAGE_NAME="${REGISTRY}${IMAGE_NAME}:${TAG}"

echo ""
echo "============================================================"
echo "  🏗️  Multi-Architecture Docker Build"
echo "============================================================"
echo "  Image: $FULL_IMAGE_NAME"
echo "  Platforms: linux/amd64, linux/arm64"
echo "  Push: $PUSH"
echo "============================================================"
echo ""

cd "$PROJECT_ROOT"

# ビルド情報の取得
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
GIT_COMMIT=${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

log_info "Build info:"
echo "  Version: $VERSION"
echo "  Git Commit: $GIT_COMMIT"
echo "  Build Date: $BUILD_DATE"
echo ""

# Docker Buildx の確認とセットアップ
log_info "Checking Docker Buildx..."
if ! docker buildx version &> /dev/null; then
    log_error "Docker Buildx is not installed"
    exit 1
fi

# Builder インスタンスの作成（存在しない場合）
BUILDER_NAME="catchup-multiarch-builder"
if ! docker buildx inspect "$BUILDER_NAME" &> /dev/null; then
    log_info "Creating buildx builder instance..."
    docker buildx create --name "$BUILDER_NAME" --driver docker-container --bootstrap
    log_success "Created builder: $BUILDER_NAME"
fi

# Builder の使用
log_info "Using builder: $BUILDER_NAME"
docker buildx use "$BUILDER_NAME"

# ビルドコマンドの構築
BUILD_CMD="docker buildx build"
BUILD_CMD="$BUILD_CMD --platform linux/amd64,linux/arm64"
BUILD_CMD="$BUILD_CMD --build-arg VERSION=$VERSION"
BUILD_CMD="$BUILD_CMD --build-arg GIT_COMMIT=$GIT_COMMIT"
BUILD_CMD="$BUILD_CMD --build-arg BUILD_DATE=$BUILD_DATE"
BUILD_CMD="$BUILD_CMD -t $FULL_IMAGE_NAME"

if [ "$PUSH" = true ]; then
    BUILD_CMD="$BUILD_CMD --push"
    log_info "Will push to registry after build"
else
    BUILD_CMD="$BUILD_CMD --load"
fi

BUILD_CMD="$BUILD_CMD ."

# ビルド実行
log_info "Starting build..."
echo "Command: $BUILD_CMD"
echo ""

if eval "$BUILD_CMD"; then
    echo ""
    log_success "Multi-architecture build completed!"

    if [ "$PUSH" = true ]; then
        log_success "Image pushed to: $FULL_IMAGE_NAME"
    else
        log_success "Image loaded locally: $FULL_IMAGE_NAME"
    fi

    # イメージ情報の表示
    echo ""
    log_info "Inspecting image..."
    docker buildx imagetools inspect "$FULL_IMAGE_NAME" 2>/dev/null || {
        log_info "Use 'docker images' to view locally loaded image"
        docker images "$IMAGE_NAME" | grep "$TAG"
    }
else
    echo ""
    log_error "Build failed!"
    exit 1
fi

echo ""
log_info "Next steps:"
if [ "$PUSH" = false ]; then
    echo "  1. Test the image: docker run --rm $FULL_IMAGE_NAME"
    echo "  2. Push to registry: $0 --push --tag $TAG"
else
    echo "  1. Update deployment to use: $FULL_IMAGE_NAME"
    echo "  2. Deploy to production"
fi
echo ""
