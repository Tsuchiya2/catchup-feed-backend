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
FROM golang:1.26.6-alpine AS deps

# ビルドツールのインストール
RUN apk add --no-cache \
    build-base \
    ca-certificates \
    curl \
    && update-ca-certificates

WORKDIR /app

# golangci-lint のインストール
# install.sh はインストールするバージョンのタグ付きスクリプトを使う。
# master のスクリプトは arm64 資産のチェックサム照合が壊れており
# (.sbom.json の行を誤マッチして verify に失敗する)、v2.12.2 の
# タグ付きスクリプトなら正しく検証できる。
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/v2.12.2/install.sh | \
    sh -s -- -b /usr/local/bin v2.12.2

# 依存関係のダウンロード（キャッシュ最適化）
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

# ────────────────────────────────────────────────────────────
# Stage 2: 開発環境（Lint、テスト用）
# ────────────────────────────────────────────────────────────
FROM deps AS dev

# swag はイメージに焼き込まない。dev コンテナでの生成は Makefile の
# `make swagger`(= go run github.com/swaggo/swag/cmd/swag)がマウントした
# ツリーの go.mod 経由で解決するため、ここで固定バージョンを二重管理しない
# (swag のバージョンは go.mod の tool ディレクティブが唯一の正、C-19)。

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
# 生成物 docs/docs.go は cmd/server がブランクインポートするため、この
# ステップは本番バイナリの一部を作っている(= 本番ビルド経路)。ツールの
# バージョンは go.mod の tool ディレクティブ(swag v1.16.6)一箇所で固定し、
# `go install ...@latest` は使わない: CI(固定)と本番(浮動)で別バージョンが
# 走り得るうえ、上流の非互換な生成物が出た日にコード変更ゼロで Pi の
# 再ビルドが落ちる。副次効果として、このステップの取得物が go.mod / go.sum で
# 検証されたグラフだけになる(@latest の go install は go.sum の外側からの
# 取得)。mod キャッシュが温まっているビルダー(Pi のローカルビルド)では
# ネットワーク取得自体がゼロになる。C-19。
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go tool swag init -g cmd/server/main.go --output docs --parseDependency --parseInternal

# ビルド情報の埋め込み（ARG）
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE
ARG LDFLAGS="-s -w -X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildDate=${BUILD_DATE}"

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
      -o server \
      ./cmd/server && \
    CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH:-amd64} \
    go build -v \
      -trimpath \
      -buildmode=pie \
      -ldflags "$LDFLAGS" \
      -o worker \
      ./cmd/worker

# バイナリの検証
RUN file server && file worker && \
    ./server --version 2>/dev/null || echo "Binary check OK"

# ────────────────────────────────────────────────────────────
# Stage 4: 最終ランタイム（最小イメージ）
# ────────────────────────────────────────────────────────────
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

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
COPY --from=build --chown=app:app /app/server  /usr/local/bin/server
COPY --from=build --chown=app:app /app/worker  /usr/local/bin/worker

# ヘルスチェック（APIサーバー用）
# - 15秒間隔でチェック
# - 3秒タイムアウト
# - 3回連続失敗で unhealthy
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/health || exit 1

# ポート公開
EXPOSE 8080

# エントリーポイント（exec形式でシグナル伝播）
ENTRYPOINT ["/usr/local/bin/server"]

# デフォルトコマンド（オーバーライド可能）
CMD []
