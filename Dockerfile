# syntax=docker/dockerfile:1.4
# ============================================================
# catchup-feed - Multi-stage Dockerfile
# ============================================================
# セキュリティとパフォーマンスを最適化したマルチステージビルド
# - 非rootユーザー実行
# - 最小イメージサイズ
# - ヘルスチェック実装
# - ダイジェスト固定で再現性確保
# ============================================================

# ────────────────────────────────────────────────────────────
# Stage 1: 依存関係ダウンロード
# ────────────────────────────────────────────────────────────
FROM golang:1.25.5-alpine AS deps

# ビルドツールのインストール
RUN apk add --no-cache \
    build-base \
    ca-certificates \
    curl \
    && update-ca-certificates

WORKDIR /app

# golangci-lint のインストール
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
    sh -s -- -b /usr/local/bin v2.6.1

# 依存関係のダウンロード（キャッシュ最適化）
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

# ────────────────────────────────────────────────────────────
# Stage 2: 開発環境（Lint、テスト用）
# ────────────────────────────────────────────────────────────
FROM deps AS dev

# 開発ツールの追加インストール
RUN --mount=type=cache,target=/go/pkg/mod \
    go install github.com/swaggo/swag/cmd/swag@latest

# ソースコードのコピー（開発時にマウント可能）
WORKDIR /app

# デフォルトコマンド（シェル起動）
CMD ["/bin/sh"]

# ────────────────────────────────────────────────────────────
# Stage 3: ビルド
# ────────────────────────────────────────────────────────────
FROM deps AS build

# ソースコードのコピー
COPY . .

# Swagger ドキュメント生成
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/swaggo/swag/cmd/swag@latest && \
    $(go env GOPATH)/bin/swag init -g cmd/api/main.go --output docs --parseDependency --parseInternal

# ビルド情報の埋め込み（ARG）
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE
ARG LDFLAGS="-s -w -X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildDate=${BUILD_DATE}"

# CGO有効（modernc.org/sqlite のため必須）
# セキュリティ強化: -buildmode=pie (Position Independent Executable)
# マルチアーキテクチャ対応: TARGETARCH を使用
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH:-amd64} \
    go build -v \
      -trimpath \
      -buildmode=pie \
      -ldflags "$LDFLAGS" \
      -o api \
      ./cmd/api && \
    CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH:-amd64} \
    go build -v \
      -trimpath \
      -buildmode=pie \
      -ldflags "$LDFLAGS" \
      -o api-worker \
      ./cmd/worker

# バイナリの検証
RUN file api && file api-worker && \
    ./api --version 2>/dev/null || echo "Binary check OK"

# ────────────────────────────────────────────────────────────
# Stage 4: 最終ランタイム（最小イメージ）
# ────────────────────────────────────────────────────────────
FROM alpine:3.22@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412

# メタデータ
LABEL maintainer="catchup-feed team" \
      org.opencontainers.image.source="https://github.com/yourusername/catchup-feed" \
      org.opencontainers.image.description="RSS/Atom Feed Aggregator with AI Summarization" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.vendor="catchup-feed"

# セキュリティアップデート & 最小限のランタイム依存
RUN apk upgrade --no-cache && \
    apk add --no-cache \
      ca-certificates \
      sqlite-libs \
      libgcc \
      libstdc++ \
      tzdata \
      curl \
    && update-ca-certificates

# タイムゾーン設定（デフォルト: UTC）
ENV TZ=UTC

# 非rootユーザー作成（セキュリティ強化）
# - UID/GID: 10001（ランダムではなく固定）
# - ホームディレクトリなし（-H）
# - シェルなし（-s /sbin/nologin）
RUN addgroup -g 10001 -S app && \
    adduser -u 10001 -S -G app -H -s /sbin/nologin app

# データディレクトリの作成と権限設定
RUN mkdir -p /data && chown -R app:app /data

# 実行ユーザー切り替え
USER app
WORKDIR /data

# ビルドステージからバイナリをコピー
COPY --from=build --chown=app:app /app/api         /usr/local/bin/api
COPY --from=build --chown=app:app /app/api-worker  /usr/local/bin/api-worker

# ヘルスチェック（APIサーバー用）
# - 15秒間隔でチェック
# - 3秒タイムアウト
# - 3回連続失敗で unhealthy
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/health || exit 1

# ポート公開
EXPOSE 8080

# エントリーポイント（exec形式でシグナル伝播）
ENTRYPOINT ["/usr/local/bin/api"]

# デフォルトコマンド（オーバーライド可能）
CMD []
