# catchup-feed プロジェクトガイド

> RSS/Atomフィードを自動クロールし、Claude APIで要約を生成、REST APIで提供するシステム
> **Go 1.25.4 / クリーンアーキテクチャ / PostgreSQL 16 / Claude Sonnet 4.5**

---

## 📖 このドキュメントについて

このドキュメントは、catchup-feedプロジェクトの**包括的なナビゲーションハブ**です。

**含まれる情報:**
- ✅ プロジェクト概要と技術スタック
- ✅ 開発環境セットアップ手順
- ✅ アーキテクチャとビジネスロジック
- ✅ Claude Codeエージェントシステム
- ✅ コーディング規約とベストプラクティス

**関連ドキュメント:**
- **Claude Code向けルール:** [.claude/CLAUDE.md](.claude/CLAUDE.md) - EDAF v1.0 エージェントシステム、7段階ゲートシステム

---

## 🚀 クイックスタート

> 💡 **推奨: Docker開発環境を使用**
> ローカルにGoをインストール不要！詳細は [docs/development-guidelines.md](docs/development-guidelines.md) を参照

### オプション A: Docker開発環境 (推奨)

```bash
# 1. 環境変数を設定
cp .env.example .env
# エディタで .env を開き、必要な値を設定

# 2. 初回セットアップ
make setup

# 3. 開発コンテナに入る
make dev-shell

# 4. (コンテナ内で) テスト実行
go test ./...

# 5. (コンテナ内で) lint実行
golangci-lint run
```

すべてのコマンドは [Makefile](Makefile) または [docs/development-guidelines.md](docs/development-guidelines.md) を参照

### オプション B: ローカル開発

#### 前提条件

```bash
# Go 1.25.4+
go version

# Docker & Docker Compose
docker --version
docker compose version

# PostgreSQL 16+ (ローカル実行の場合のみ)
psql --version
```

### 2. 環境変数設定

```bash
# サンプルファイルをコピー
cp .env.example .env

# エディタで .env を開き、必要な値を設定
```

**必須項目:**
- `JWT_SECRET`: `openssl rand -base64 64` で生成（32文字以上）
- `ANTHROPIC_API_KEY`: 実際のAPIキーを設定（claude使用時）
- `OPENAI_API_KEY`: 実際のAPIキーを設定（openai使用時）
- `ADMIN_USER_PASSWORD`: 強力なパスワードに変更
- `CORS_ALLOWED_ORIGINS`: 許可するオリジン（開発時: `http://localhost:3000,http://localhost:3001`）

#### 環境変数の主要項目

| 項目 | 説明 | 例 |
|------|------|-----|
| `DATABASE_URL` | PostgreSQL接続文字列 | `postgres://user:pass@host:5432/db` |
| `JWT_SECRET` | JWT署名用秘密鍵（32文字以上必須） | `openssl rand -base64 64` で生成 |
| `SUMMARIZER_TYPE` | 要約エンジン (`openai` or `claude`) | `openai` |
| `SUMMARIZER_CHAR_LIMIT` | 要約の最大文字数（範囲: 100-5000） | `900` (デフォルト) |
| `OPENAI_API_KEY` | OpenAI APIキー | `sk-proj-...` |
| `ANTHROPIC_API_KEY` | Anthropic APIキー | `sk-ant-...` |
| `ADMIN_USER` | 管理者ユーザー名 | `admin` |
| `ADMIN_USER_PASSWORD` | 管理者パスワード | 強力なパスワード |
| `DEMO_USER` | ビューワーロールのユーザー名（オプション） | `demo` |
| `DEMO_USER_PASSWORD` | ビューワーロールのパスワード（オプション） | `demo123` |
| `LOG_LEVEL` | ログレベル (`debug`, `info`) | `info` |
| `DISCORD_ENABLED` | Discord通知の有効化 | `true` or `false` (デフォルト: `false`) |
| `DISCORD_WEBHOOK_URL` | Discord Webhook URL | `https://discord.com/api/webhooks/...` (DISCORD_ENABLED=true時に必須) |
| `SLACK_ENABLED` | Slack通知の有効化 | `true` or `false` (デフォルト: `false`) |
| `SLACK_WEBHOOK_URL` | Slack Webhook URL | `https://hooks.slack.com/services/...` (SLACK_ENABLED=true時に必須) |
| `NOTIFY_MAX_CONCURRENT` | 最大同時通知数 | `10` (デフォルト、範囲: 1-100) |
| `METRICS_PORT` | メトリクスHTTPサーバーのポート | `9090` (デフォルト) |
| `CONTENT_FETCH_ENABLED` | コンテンツ取得機能の有効化 | `true` or `false` (デフォルト: `true`) |
| `CONTENT_FETCH_THRESHOLD` | コンテンツ取得の閾値（文字数） | `1500` (デフォルト) |
| `CONTENT_FETCH_TIMEOUT` | コンテンツ取得のタイムアウト | `10s` (デフォルト) |
| `CONTENT_FETCH_PARALLELISM` | 最大同時コンテンツ取得数 | `10` (デフォルト、範囲: 1-50) |
| `CONTENT_FETCH_MAX_BODY_SIZE` | 最大レスポンスサイズ（バイト） | `10485760` (10MB、デフォルト) |
| `CONTENT_FETCH_MAX_REDIRECTS` | 最大リダイレクト数 | `5` (デフォルト、範囲: 0-10) |
| `CONTENT_FETCH_DENY_PRIVATE_IPS` | プライベートIPアクセス拒否 | `true` or `false` (デフォルト: `true`) |

#### 要約エンジンの選択

環境変数 `SUMMARIZER_TYPE` で要約エンジンを選択できます：

| エンジン | 推奨用途 | 月額コスト | 必要な環境変数 |
|----------|----------|-----------|---------------|
| `openai` | **開発環境** | 約200円/1,000記事 | `OPENAI_API_KEY` |
| `claude` | **本番環境** | 約1,400円/1,000記事 | `ANTHROPIC_API_KEY` |

**推奨設定:**
- 開発環境: `SUMMARIZER_TYPE=openai` （コスト優先）
- 本番環境: `SUMMARIZER_TYPE=claude` （品質優先）

#### 要約文字数制限の設定

`SUMMARIZER_CHAR_LIMIT` 環境変数で、AI生成される要約の最大文字数を制御できます：

**設定値:**
- **デフォルト**: `900` 文字（日本語文字数）
- **有効範囲**: `100` ～ `5000` 文字
- **範囲外の値**: 自動的にデフォルト（900）にフォールバック（警告ログ出力）
- **無効な値**: 自動的にデフォルト（900）にフォールバック（警告ログ出力）

**動作:**
- AIに対するプロンプトに文字数制限が含まれます（例: "900文字以内で要約してください"）
- 実際の要約文字数はメトリクスとログで追跡されます
- 制限を超えた要約も保存されますが、警告ログが出力されます
- 目標コンプライアンス率: ≥95%

**使用例:**
```bash
# デフォルト（900文字）
# SUMMARIZER_CHAR_LIMIT は未設定でOK

# カスタム設定（500文字）
export SUMMARIZER_CHAR_LIMIT=500

# カスタム設定（1200文字）
export SUMMARIZER_CHAR_LIMIT=1200
```

#### RSS Content Enhancement（NEW）

**概要:** AI要約の品質向上のため、RSSフィードの内容が不十分な場合に自動的に元記事のフルテキストを取得する機能

**背景:**
- 約50%のRSSフィード（B級フィード）は記事の要約のみを提供
- 要約だけではAI要約の品質が低下（品質が良いフィードは全体の40%のみ）
- フルテキストを取得することで、AI要約の品質を90%まで向上可能

**動作:**
1. RSSコンテンツの長さをチェック（閾値: 1,500文字）
2. 不十分な場合、元記事URLから自動的にフルテキストを取得
3. Mozilla Readabilityアルゴリズムで記事本文を抽出
4. 取得に失敗した場合はRSSコンテンツにフォールバック

**セキュリティ機能:**
- **SSRF防止**: プライベートIPアドレスへのアクセスをブロック
- **サイズ制限**: 最大10MBまでのレスポンスのみ許可
- **タイムアウト**: リクエストごとに10秒のタイムアウト
- **リダイレクト制限**: 最大5回までのリダイレクトを許可
- **サーキットブレーカー**: 連続失敗時の自動遮断

**パフォーマンス:**
- 10並列でコンテンツ取得（I/O バウンド）
- 5並列でAI要約（レート制限）
- 2層の並列処理で効率化
- クロール時間: 約2〜5分（フィード数による）

**環境変数:**
- `CONTENT_FETCH_ENABLED`: 機能の有効化（デフォルト: true）
- `CONTENT_FETCH_THRESHOLD`: 取得する閾値（デフォルト: 1500文字）
- `CONTENT_FETCH_TIMEOUT`: タイムアウト（デフォルト: 10s）
- `CONTENT_FETCH_PARALLELISM`: 並列度（デフォルト: 10）
- `CONTENT_FETCH_MAX_BODY_SIZE`: 最大サイズ（デフォルト: 10MB）
- `CONTENT_FETCH_MAX_REDIRECTS`: リダイレクト上限（デフォルト: 5）
- `CONTENT_FETCH_DENY_PRIVATE_IPS`: SSRF防止（デフォルト: true）

**メトリクス:**
```promql
# コンテンツ取得成功率
sum(rate(content_fetch_attempts_total{result="success"}[5m]))
/
sum(rate(content_fetch_attempts_total[5m]))

# 取得時間（p95）
histogram_quantile(0.95, rate(content_fetch_duration_seconds_bucket[5m]))

# コンテンツサイズ分布
histogram_quantile(0.95, rate(content_fetch_size_bytes_bucket[5m]))
```

**無効化方法:**
```bash
# .env に追加
CONTENT_FETCH_ENABLED=false
```

### 3. Docker Composeで実行（推奨）

```bash
# コンテナを起動
docker compose up -d --build

# ログ確認
docker compose logs -f app
```

#### コンテナの管理

```bash
# 停止
docker compose down

# 停止してボリュームも削除（DB初期化）
docker compose down -v

# 再ビルド
docker compose up -d --build

# ⚠️ 開発コンテナ(dev)も含めて停止する場合
# devサービスはprofile指定されているため、通常のdownでは対象外となり、
# ネットワークが削除されない場合があります
docker compose --profile dev down

# または、すべてのリソースを完全削除
docker compose down --volumes --remove-orphans

# ネットワークが残ってしまった場合の手動削除
docker network rm catchup-feed_backend
```

### 4. ローカル実行（Go直接実行）

```bash
# 依存関係のインストール
go mod download

# APIサーバーを起動（ターミナル1）
go run ./cmd/api

# ワーカーを起動（ターミナル2）
go run ./cmd/worker
```

### 5. アクセス方法

サービスが起動したら、以下のURLにアクセスできます：

| サービス | URL | 説明 |
|---------|-----|------|
| **API** | http://localhost:8080 | REST API |
| **Swagger UI** | http://localhost:8080/swagger/index.html | API ドキュメント・テスト |
| **Health Check** | http://localhost:8080/health | API ヘルスチェック |
| **Metrics** | http://localhost:8080/metrics | API Prometheus メトリクス |
| **Worker Health** | http://localhost:9091/health | Worker ヘルスチェック |
| **Worker Metrics** | http://localhost:9091/metrics | Worker Prometheus メトリクス |
| **Prometheus** | http://localhost:9090 | メトリクス収集 |
| **Grafana** | http://localhost:3000 | ダッシュボード（admin/admin） |

---

## 📱 主要機能

- RSS/Atomフィードの自動クロール（デフォルト: 毎日5:30 AM、CRON_SCHEDULEで設定可能）
- Claude/OpenAI APIによる記事要約の自動生成
- **NEW:** RSS Content Enhancement - フルテキスト自動取得によるAI要約品質向上（40% → 90%）
- **NEW:** Crawl Resilience - 個別記事の要約エラーがあっても全ソースをクロール（詳細: [CHANGELOG.md](CHANGELOG.md)）
- **NEW:** Feed Quality Management - 問題のあるフィード（404エラー、パーサー非互換）を自動検出・無効化（24/32フィード稼働中、成功率75%）
- JWT認証によるセキュアなREST API
- URL重複検知による記事の重複防止
- PostgreSQL/SQLite対応のリポジトリパターン
- クリーンアーキテクチャ設計
- 並列処理によるフィード取得の最適化
- マルチチャネル通知システム（Discord、Slack、将来的にEmail、Telegram対応）
- Prometheus メトリクス収集とヘルスチェックエンドポイント

### ロールベースアクセス制御（RBAC）

APIは2つのユーザーロールをサポートしています：

- **Admin**: すべてのエンドポイントへのフルアクセス（作成、参照、更新、削除）
- **Viewer**: デモや監視用途向けの読み取り専用アクセス

ビューワーアクセスを設定するには、`.env` ファイルで `DEMO_USER` と `DEMO_USER_PASSWORD` を設定してください。

---

## 💻 技術スタック

### バックエンド
- **Go:** 1.25.4
- **データベース:** PostgreSQL 16 / SQLite (テスト用)
- **HTTPルーター:** 標準 net/http
- **認証:** JWT (golang-jwt/jwt/v5)
- **主要ライブラリ:**
  - `jackc/pgx/v5` - PostgreSQLドライバ
  - `mmcdole/gofeed` - RSS/Atom解析
  - `robfig/cron/v3` - スケジューラ
  - `anthropics/anthropic-sdk-go` - Anthropic Claude APIクライアント
  - `go-shiori/go-readability` - Mozilla Readabilityアルゴリズム実装（フルテキスト抽出）

### 外部サービス
- **AI要約:** Anthropic Claude (Sonnet 4.5) / OpenAI (GPT-4o-mini)

### 開発ツール
- **テスト:** testing / testify
- **Lint/Format:** go fmt / goimports / go vet / golangci-lint
- **CI/CD:** GitHub Actions
- **監視:** Prometheus / Grafana
- **ドキュメント:** Swagger (swaggo/swag)

---

## 🏗️ アーキテクチャ概要

### クリーンアーキテクチャ

```
┌────────────────────────────────┐
│ Presentation (HTTP Handlers)   │  ← cmd/api, internal/handler/http
├────────────────────────────────┤
│ UseCase (Business Logic)       │  ← internal/usecase
├────────────────────────────────┤
│ Domain (Entities)              │  ← internal/domain/entity
├────────────────────────────────┤
│ Infrastructure (DB, API)       │  ← internal/infra
└────────────────────────────────┘
```

**依存方向:** 外側 → 内側（Presentation → UseCase → Domain）

### 基本設計原則

1. **クリーンアーキテクチャ準拠** - 依存方向を厳守（外側 → 内側）
2. **Domain層の独立性** - 外部依存を持たない（標準ライブラリのみ）
3. **依存性逆転の原則** - インターフェースを活用（repository, summarizer）
4. **テスタビリティ** - モックを使った単体テスト
5. **単一責任の原則** - 1つのユースケースは1つの責務

### 主要コンポーネント

#### cmd/api - APIサーバー

- **役割:** HTTPサーバー起動、ルーティング設定
- **ポート:** 8080
- **エンドポイント:**
  - `/auth/token` - JWT認証トークン取得
  - `/sources` - フィードソース管理
  - `/articles` - 記事管理
  - `/health` - ヘルスチェック
  - `/metrics` - Prometheusメトリクス

#### cmd/worker - バッチクローラー

- **役割:** 定期的なフィード取得・要約生成
- **実行間隔:** デフォルト毎日5:30 AM（cron: `30 5 * * *`、環境変数 `CRON_SCHEDULE` で設定可能）
- **処理フロー:**
  1. 全ソースを取得
  2. 並列でフィードを取得
  3. 新規記事のみ保存
  4. Claude/OpenAI APIで要約生成
  5. 記事を更新

#### internal/domain - ドメイン層

- **Article:** 記事エンティティ（ID, Title, Summary, URL, PublishedAt等）
- **Source:** フィードソースエンティティ（ID, Name, FeedURL, IsActive等）
- **User:** ユーザーエンティティ（ID, Username, PasswordHash等）

**重要:** Domain層は外部依存を持たない（標準ライブラリのみ）

#### internal/usecase - ユースケース層

- **article/** - 記事ユースケース（作成、一覧取得、詳細取得）
- **source/** - ソースユースケース（作成、一覧取得）
- **auth/** - 認証ユースケース（JWT認証）
- **fetch/** - フィード取得・要約生成
- **notify/** - **NEW:** マルチチャネル通知システム

#### internal/handler/http - HTTPハンドラ

- **article_handler.go** - 記事API（Create, List, GetByID）
- **source_handler.go** - ソースAPI（Create, List）
- **auth_handler.go** - 認証API（Login）

#### internal/infra - インフラ層

- **db/postgres/** - PostgreSQL実装
- **db/sqlite/** - SQLite実装
- **summarizer/claude.go** - Claude API統合
- **summarizer/openai.go** - OpenAI API統合
- **feed/parser.go** - RSS/Atom解析
- **notifier/** - 通知インフラ実装（Discord、Slack、将来的にEmail、Telegram）

### 通知システムアーキテクチャ (NEW)

**概要:** マルチチャネル対応の拡張可能な通知システム

```
┌──────────────────────────────────────────────────────────────┐
│ usecase/fetch/service.go                                     │
│   └─ NotifyNewArticle(article) ────────────────┐             │
└────────────────────────────────────────────────┼─────────────┘
                                                 │
                                                 ▼
┌──────────────────────────────────────────────────────────────┐
│ usecase/notify/service.go (Notification Service)             │
│   ├─ NotifyNewArticle(ctx, article)                          │
│   ├─ Goroutine Pool (max concurrent: 10)                     │
│   └─ Dispatch to Channels ──────────────┐                    │
└──────────────────────────────────────────┼───────────────────┘
                                           │
                    ┌──────────────────────┼──────────────────────┐
                    ▼                      ▼                      ▼
        ┌───────────────────┐  ┌───────────────────┐  ┌───────────────────┐
        │ Discord Channel   │  │ Slack Channel     │  │ Email Channel     │
        │ (Phase 1)         │  │ (Future)          │  │ (Future)          │
        ├───────────────────┤  ├───────────────────┤  ├───────────────────┤
        │ - Circuit Breaker │  │ - Circuit Breaker │  │ - Circuit Breaker │
        │ - Rate Limiter    │  │ - Rate Limiter    │  │ - Rate Limiter    │
        │ - Retry Logic     │  │ - Retry Logic     │  │ - Retry Logic     │
        └─────────┬─────────┘  └─────────┬─────────┘  └─────────┬─────────┘
                  │                      │                      │
                  ▼                      ▼                      ▼
        ┌───────────────────┐  ┌───────────────────┐  ┌───────────────────┐
        │ Discord API       │  │ Slack API         │  │ SMTP Server       │
        └───────────────────┘  └───────────────────┘  └───────────────────┘

                              Observability
                    ┌────────────────────────────────┐
                    │ Metrics (Prometheus)           │
                    │  - notification_sent_total     │
                    │  - notification_duration_secs  │
                    │  - notification_rate_limit_hit │
                    │  - circuit_breaker_open        │
                    ├────────────────────────────────┤
                    │ Health Checks (HTTP)           │
                    │  - GET /health                 │
                    │  - GET /health/channels        │
                    │  - GET /metrics                │
                    └────────────────────────────────┘
```

**主要機能:**
- **マルチチャネル対応**: Discord（実装済）、Slack（実装済）、Email、Telegram（将来対応）
- **Circuit Breaker**: 連続失敗時の自動遮断（5回失敗で1分間遮断）
- **Rate Limiter**: チャネルごとの独立したレート制限（Discord: 2 req/s, Slack: 1 req/s）
- **Goroutine Pool**: 最大同時通知数の制御（デフォルト: 10）
- **Prometheus Metrics**: 成功率、レイテンシ、レート制限ヒット数等を記録
- **Health Check Endpoints**: サービスとチャネルの健全性監視

**メトリクス例:**
```promql
# 成功率（過去5分間）
sum(rate(notification_sent_total{status="success"}[5m])) by (channel)
/
sum(rate(notification_sent_total[5m])) by (channel)

# p95レイテンシ
histogram_quantile(0.95,
  sum(rate(notification_duration_seconds_bucket[5m])) by (channel, le)
)

# サーキットブレーカー状態
sum(increase(notification_circuit_breaker_open_total[1h])) by (channel)
```

---

## 🔄 Migration Notes (Notification System)

**既存デプロイメントからの移行:**

通知システムのリファクタリングは **後方互換性** があり、既存のデプロイメントに影響を与えません。

### 変更点

**1. 新しい環境変数（オプション）:**
- `NOTIFY_MAX_CONCURRENT`: 最大同時通知数（デフォルト: 10）
- `METRICS_PORT`: メトリクスサーバーのポート（デフォルト: 9090）

既存の `DISCORD_ENABLED` と `DISCORD_WEBHOOK_URL` は引き続き使用されます。

**2. 新しいメトリクスエンドポイント:**
- `http://localhost:9090/metrics` - Prometheus メトリクス（新規）
- `http://localhost:9090/health` - ライブネスプローブ（新規）
- `http://localhost:9090/health/channels` - チャネル健全性チェック（新規）

**3. 新しいアーキテクチャコンポーネント:**
- `internal/usecase/notify/` - 新しい通知ユースケース層
- `cmd/worker/metrics_server.go` - メトリクスHTTPサーバー

**4. 変更されたコンポーネント:**
- `internal/usecase/fetch/service.go` - 通知ロジックを `notify` サービスに委譲
- `internal/infra/notifier/` - インフラ層（変更なし、引き続き使用）

### 移行手順

**ステップ1: コードデプロイ**
```bash
# 最新コードをプル
git pull origin main

# 再ビルド＆再起動
docker compose down
docker compose up -d --build
```

**ステップ2: メトリクスエンドポイント確認**
```bash
# メトリクスエンドポイントが応答することを確認
curl http://localhost:9090/metrics | grep "notification_"

# ヘルスチェック確認
curl http://localhost:9090/health
curl http://localhost:9090/health/channels
```

**ステップ3: Prometheus設定更新（オプション）**
```yaml
# prometheus.yml に追加
scrape_configs:
  - job_name: 'worker'
    static_configs:
      - targets: ['app:9090']
```

**ステップ4: アラート設定（推奨）**

Prometheusアラートルールを設定して、通知システムの健全性を監視してください。

### 既存機能への影響

- ✅ **Discord通知**: 引き続き正常に動作（動作変更なし）
- ✅ **フィード取得**: 通知との疎結合化により、より堅牢に
- ✅ **データベース**: スキーマ変更なし
- ✅ **API**: 変更なし
- ✅ **認証**: 変更なし

### ロールバック

万が一問題が発生した場合は、以前のコミットに戻すだけで元の動作に復元できます：

```bash
# 以前のバージョンに戻す
git checkout <previous-commit-hash>
docker compose down
docker compose up -d --build
```

---

## 📚 ビジネスロジック概要

### 記事取得フロー

**概要:** 定期的にRSS/Atomフィードから新規記事を取得し、要約を生成

**処理フロー:**
1. **ソース取得:** 有効な全ソースを取得（`IsActive = true`）
2. **並列フィード取得:** 各ソースのフィードを並列で取得
3. **重複チェック:** URLで既存記事を検索
4. **新規記事保存:** 重複していない場合のみ保存
5. **要約生成:** Claude/OpenAI APIで要約生成
6. **記事更新:** 要約を記事に反映

**主要モデル:**
- Source - フィードソース
- Article - 記事

### JWT認証

**概要:** JWT（JSON Web Token）による認証

**認証フロー:**
1. **ログイン:** `/auth/token` にusername/passwordをPOST
2. **トークン発行:** JWTトークンを発行（有効期限: 24時間）
3. **認証:** `Authorization: Bearer <TOKEN>` ヘッダーで認証

**トークンペイロード:**
- `sub`: ユーザー名
- `role`: ユーザーロール（admin）
- `exp`: 有効期限（24時間）

**セキュリティ:**
- ✅ **v2.0新機能**: すべてのHTTPメソッド（GET含む）で保護エンドポイントへのアクセスにJWT認証が必須
- ✅ **起動時認証情報検証**: 弱いパスワードや空のパスワードでサーバー起動を防止
- ✅ **JWT署名検証**: HS256アルゴリズムによる署名検証
- ✅ **秘密鍵管理**: 環境変数 `JWT_SECRET` による安全な秘密鍵管理
- ✅ **パスワードポリシー**: 12文字以上、弱いパスワードパターン検出

**パスワード要件** (v2.0+):
- 最低12文字以上
- 弱いパスワード（admin, password等）は禁止
- 単純な数値パターン（111111111111, 123456789012）は禁止
- キーボードパターン（qwertyuiop, asdfghjkl）は禁止

**保護エンドポイント** (JWT認証必須):
- `GET/POST/PUT/DELETE /articles/*` - すべての記事操作
- `GET/POST/PUT/DELETE /sources/*` - すべてのソース操作

**公開エンドポイント** (認証不要):
- `POST /auth/token` - トークン生成
- `GET /health`, `/ready`, `/live` - ヘルスチェック
- `GET /metrics` - Prometheusメトリクス
- `GET /swagger/*` - API ドキュメント

**破壊的変更** (v2.0):
- **v1.x**: GETリクエストは認証不要
- **v2.0**: GETリクエストを含むすべてのメソッドで保護エンドポイントへのアクセスにJWT認証が必須

---

## 📡 API使用例

### 認証トークンの取得

```bash
curl -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your-password"}'

# レスポンス例:
# {"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

### フィードソースの作成

```bash
# トークンを取得
TOKEN=$(curl -s -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your-password"}' \
  | jq -r '.token')

# トークンを使用してソース作成
curl -X POST http://localhost:8080/sources \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Go Blog","feedURL":"https://go.dev/blog/feed.atom"}'
```

### ソース一覧の取得

```bash
# v2.0以降: JWT認証が必須
curl http://localhost:8080/sources \
  -H "Authorization: Bearer $TOKEN"
```

### 記事一覧の取得

```bash
# v2.0以降: JWT認証が必須
curl http://localhost:8080/articles \
  -H "Authorization: Bearer $TOKEN"
```

詳細なAPI仕様は [Swagger UI](http://localhost:8080/swagger/index.html) を参照してください。

---

## 🎯 開発ルール

### Gitブランチルール

```bash
# 開発用
git checkout -b feature/XXX main

# バグ修正
git checkout -b fix/XXX main

# ホットフィックス
git checkout -b hotfix/XXX main

# PR
# main（開発・本番リリース）に向けて作成
```

**コミットメッセージ:**
- `feat:` - 新機能
- `fix:` - バグ修正
- `docs:` - ドキュメント変更
- `refactor:` - リファクタリング
- `test:` - テスト追加・修正

### コーディング規約

#### クリーンアーキテクチャ準拠

**依存方向を厳守:**
- ✅ Handler → UseCase → Domain
- ❌ Domain → UseCase（禁止）
- ❌ Domain → Infra（禁止）

**インターフェース活用:**
- Repository（`internal/repository/`）
- Summarizer（`internal/domain/service/`）

#### Go標準規約

```bash
# フォーマット（必須）
go fmt ./...

# import整理（推奨）
goimports -w .

# 静的解析（必須）
go vet ./...

# golangci-lint（推奨）
golangci-lint run
```

#### エラーハンドリング

```go
// ✅ 良い例
if err != nil {
    return fmt.Errorf("failed to create article: %w", err)
}

// ❌ 悪い例
if err != nil {
    return err  // コンテキスト情報なし
}
```

#### コンテキスト

```go
// ✅ 良い例
func (uc *CreateArticleUseCase) Execute(ctx context.Context, req CreateArticleRequest) (*Article, error) {
    // context.Contextを第1引数に
}

// ❌ 悪い例
func (uc *CreateArticleUseCase) Execute(req CreateArticleRequest) (*Article, error) {
    // context.Contextなし
}
```

#### ドキュメント

```go
// ✅ 良い例
// CreateArticle creates a new article with the given parameters.
// It returns the created article or an error if the operation fails.
func CreateArticle(ctx context.Context, req CreateArticleRequest) (*Article, error) {
    // ...
}

// ❌ 悪い例
func CreateArticle(ctx context.Context, req CreateArticleRequest) (*Article, error) {
    // コメントなし
}
```

### テスト

- **カバレッジ:** 70%以上
- **テーブル駆動テスト:** 使用推奨
- **go test:** 全テスト通過が必須

```bash
# 全テスト実行
go test ./...

# カバレッジ付き
go test ./... -cover

# 特定パッケージ
go test ./internal/usecase/...

# レースディテクター
go test ./... -race

# 詳細出力
go test ./... -v
```

---

## 🤖 Claude Code EDAF v1.0 エージェントシステム

### EDAF v1.0 - 7段階ゲートシステム

catchup-feedプロジェクトでは、**EDAF v1.0**（Evaluator-Driven Agent Flow）を採用しています。

> 自然言語で機能実装を依頼するだけで、7段階のゲートシステムが自動実行されます。
> 「EDAF」というキーワードは不要です。

**適用場面:**
- ✅ 新機能追加・リファクタリング
- ✅ 認証・セキュリティ関連のクリティカルなタスク
- ✅ 高品質保証が必要なタスク

### EDAF 7-Phase Gate System

| Phase | Agent | Evaluators | Pass Criteria |
|-------|-------|------------|---------------|
| 1. Requirements | requirements-gatherer | 7 | All ≥ 8.0/10 |
| 2. Design | designer | 7 | All ≥ 8.0/10 |
| 3. Planning | planner | 7 | All ≥ 8.0/10 |
| 4. Implementation | 6 workers | 1 quality-gate | 10.0 (lint+tests) |
| 5. Code Review | - | 8 | All ≥ 8.0/10 |
| 6. Documentation | documentation-worker | 5 | All ≥ 8.0/10 |
| 7. Deployment | - | 5 | All ≥ 8.0/10 |

**Phase 1: Requirements Gate（要件定義）**
- requirements-gatherer エージェント → 要件書作成（`.steering/{date}-{feature}/idea.md`）
- 7つのRequirements Evaluators → 並列評価（明確性、スコープ、実現可能性等）
- 全評価 ≥ 8.0/10.0 で次フェーズへ

**Phase 2: Design Gate（設計）**
- designer エージェント → 設計書作成（`.steering/{date}-{feature}/design.md`）
- 7つのDesign Evaluators → 並列評価（一貫性、拡張性、保守性等）
- 全評価 ≥ 8.0/10.0 で次フェーズへ

**Phase 3: Planning Gate（計画）**
- planner エージェント → タスク計画作成（`.steering/{date}-{feature}/tasks.md`）
- 7つのPlanner Evaluators → 並列評価（明確性、粒度、依存関係等）
- 全評価 ≥ 8.0/10.0 で次フェーズへ

**Phase 4: Implementation Gate（実装）**
- Worker エージェント → コード実装
  - `database-worker-v1-self-adapting` - データベース実装
  - `backend-worker-v1-self-adapting` - バックエンド実装
  - `frontend-worker-v1-self-adapting` - フロントエンド実装
  - `test-worker-v1-self-adapting` - テスト実装
  - `documentation-worker` - ドキュメント実装
  - `ui-verification-worker` - UI検証
- quality-gate-evaluator → 10.0必須（lint警告ゼロ + 全テスト通過）

**Phase 5: Code Review Gate（レビュー）** ← **絶対にスキップ禁止**
- 8つのCode Evaluators → 並列評価（品質、セキュリティ、パフォーマンス等）
- standards-compliance-evaluator → プロジェクト固有のコーディング規約チェック
- フロントエンド変更時は UI/UX 検証必須
- 全評価 ≥ 8.0/10.0 で次フェーズへ

**Phase 6: Documentation Gate（ドキュメント）**
- documentation-worker → 永続ドキュメント更新（`docs/`）
- 5つのDocumentation Evaluators → 並列評価（正確性、完全性、明確性等）
- 全評価 ≥ 8.0/10.0 で次フェーズへ

**Phase 7: Deployment Gate（デプロイ）** - オプション
- 5つのDeployment Evaluators → 並列評価（準備状況、セキュリティ、監視等）
- 全評価 ≥ 8.0/10.0 でデプロイ承認

**詳細:** [.claude/CLAUDE.md](.claude/CLAUDE.md)

### Claude Code への依頼例

**✅ 良い依頼例**

```
# 新機能の追加
「記事にタグ機能を追加してください。
要件：
- タグの作成・更新・削除・一覧取得API
- 記事とタグの多対多関連
- タグでの記事検索機能
- PostgreSQLで永続化
- テストも作成してください」

→ EDAF 4-Phase Gate System に従って自動実行
```

```
# パフォーマンス改善
「記事一覧APIのパフォーマンスを改善してください。
現状：レスポンスが遅い（500ms以上）
目標：200ms以内
制約：既存APIとの互換性を保つこと」

→ Designer で改善策を設計 → Backend Worker で実装 → Code Evaluators で検証
```

```
# バグ修正
「ワーカーが重複記事を登録してしまう問題を修正してください。
再現手順：
1. 同じフィードを2回連続でクロール
2. 同じ記事が2件登録される
期待：URL重複チェックが機能すること」

→ Backend Worker で修正 → Code Evaluators で検証
```

```
# セキュリティ強化
「JWT認証のセキュリティを強化してください。
要件：
- トークンのリフレッシュ機構
- トークン失効機能
- セキュリティベストプラクティスに準拠」

→ Designer → Backend Worker → Code Evaluators（セキュリティ評価含む）
```

```
# リファクタリング
「internal/usecase/fetch/service.goをリファクタリングしてください。
目的：可読性と保守性の向上
制約：既存のテストが全て通ること」

→ Backend Worker → Code Evaluators（コード品質・保守性評価）
```

**❌ 避けるべき依頼例**

```
「良い感じにして」
→ 要件が不明確

「タグ機能」
→ 具体的な要求が不足

「バグを直して」
→ どのバグか不明
```

**💡 ポイント**

- **要件を明確に**: 何を実現したいか具体的に
- **制約を伝える**: 既存APIとの互換性、パフォーマンス要求等
- **期待する成果物**: テストが必要か、ドキュメント更新が必要か
- **背景情報**: なぜ必要か、現状の問題は何か

---

## 📂 ディレクトリ構造

```
catchup-feed/
├── cmd/
│   ├── api/                  # APIサーバー（ポート8080）
│   └── worker/               # バッチクローラー（毎日5:30 AM）
├── internal/
│   ├── domain/
│   │   └── entity/           # ドメインエンティティ（Article, Source, User）
│   ├── service/              # ドメインサービス（Summarizer）
│   ├── repository/           # リポジトリインターフェース
│   ├── usecase/              # ビジネスロジック層
│   │   ├── article/          # 記事ユースケース
│   │   ├── source/           # ソースユースケース
│   │   ├── auth/             # 認証ユースケース
│   │   ├── fetch/            # フィード取得ユースケース
│   │   └── notify/           # 通知ユースケース
│   ├── handler/
│   │   └── http/             # HTTPハンドラ
│   │       ├── article/      # 記事ハンドラ
│   │       ├── source/       # ソースハンドラ
│   │       ├── auth/         # 認証ハンドラ
│   │       ├── respond/      # レスポンスユーティリティ
│   │       ├── pathutil/     # パスユーティリティ
│   │       └── requestid/    # リクエストID管理
│   ├── infra/
│   │   ├── adapter/          # 永続化アダプタ（PostgreSQL, SQLite）
│   │   ├── summarizer/       # 要約エンジン実装（Claude, OpenAI）
│   │   ├── scraper/          # RSS/Atom解析
│   │   ├── fetcher/          # コンテンツ取得（Readability）
│   │   └── notifier/         # 通知インフラ実装（Discord、Slack）
│   ├── observability/        # 監視・ロギング
│   ├── resilience/           # サーキットブレーカー・リトライ
│   ├── config/               # 設定管理
│   └── pkg/                  # 共通パッケージ
├── docs/                     # プロジェクトドキュメント（永続）
│   ├── product-requirements.md   # 製品要件
│   ├── functional-design.md      # 機能設計
│   ├── architecture.md           # アーキテクチャ
│   ├── development-guidelines.md # 開発ガイドライン
│   ├── repository-structure.md   # リポジトリ構造
│   └── glossary.md               # 用語集
├── .steering/                # EDAF作業成果物（機能ごと）
│   └── {date}-{feature}/     # 例: 2026-01-09-user-tags
│       ├── idea.md           # Phase 1: 要件書
│       ├── design.md         # Phase 2: 設計書
│       ├── tasks.md          # Phase 3: タスク計画
│       └── reports/          # Phase 5: 評価レポート
├── .claude/                  # EDAF v1.0 設定
│   ├── CLAUDE.md             # EDAF v1.0 システムガイド
│   ├── agents/               # エージェント定義
│   │   ├── designer.md       # 設計エージェント
│   │   ├── planner.md        # 計画エージェント
│   │   ├── requirements-gatherer.md  # 要件収集エージェント
│   │   ├── workers/          # ワーカーエージェント（6個）
│   │   └── evaluators/       # 評価エージェント（40個）
│   │       ├── phase1-requirements/  # 要件評価（7個）
│   │       ├── phase2-design/        # 設計評価（7個）
│   │       ├── phase3-planner/       # 計画評価（7個）
│   │       ├── phase4-quality-gate/  # 品質ゲート（1個）
│   │       ├── phase5-code/          # コード評価（8個）
│   │       ├── phase6-documentation/ # ドキュメント評価（5個）
│   │       └── phase7-deployment/    # デプロイ評価（5個）
│   ├── skills/               # コーディング規約・ワークフロー
│   │   ├── go-standards/     # Go言語規約
│   │   ├── test-standards/   # テスト規約
│   │   ├── security-standards/   # セキュリティ規約
│   │   ├── edaf-orchestration/   # EDAFワークフロー
│   │   ├── edaf-evaluation/      # 評価パターン
│   │   └── ui-verification/      # UI検証
│   ├── commands/             # カスタムコマンド（/setup, /review-standards）
│   ├── settings.json         # Claude Code設定
│   └── edaf-config.yml       # EDAF v1.0 言語・プロジェクト設定
├── README.md                 # このファイル（包括的ガイド）
├── CHANGELOG.md              # セマンティックバージョニング準拠の変更記録
└── AGENTS.md                 # リポジトリガイドラインと規約
```

---

## 🔑 重要な制約事項

### 技術的制約

1. **クリーンアーキテクチャ準拠** - 依存方向を厳守（外側 → 内側）
2. **Domain層の独立性** - 外部依存を持たない（標準ライブラリのみ）
3. **Git書き込み操作禁止** - Claude Codeはgit add/commit/push等の書き込み操作禁止（読み取り専用のgit status/diff/logのみ可）
4. **テストカバレッジ** - 70%以上を維持
5. **静的解析ツール準拠** - go fmt / goimports / go vet / golangci-lint

### セキュリティ制約

1. **環境変数管理** - `.env`はGitにコミット禁止
2. **API Key管理** - `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` はGitにコミット禁止
3. **JWT秘密鍵** - `JWT_SECRET` はGitにコミット禁止（32文字以上必須）
4. **パスワードハッシュ化** - bcrypt使用
5. **HTTPSのみ** - 本番環境ではHTTPSのみ許可

### ビジネス制約

1. **フィード取得間隔:** デフォルト毎日5:30 AM（環境変数 `CRON_SCHEDULE` で変更可能）
2. **要約最大トークン数:** 150
3. **JWT有効期限:** 24時間
4. **並列フィード取得数:** 制限なし（goroutine使用）

---

## 🔍 よくあるタスク

### 新機能開発

```bash
# 1. 最新のmainを取得
git checkout main
git pull origin main

# 2. ブランチ作成
git checkout -b feature/XXX main

# 3. 実装（Domain → Repository → UseCase → Handler）
# 4. テスト作成
# 5. 静的解析
go fmt ./...
goimports -w .
go vet ./...

# 6. テスト実行
go test ./... -cover

# 7. PR作成（main へ）
```

### Swagger更新

```bash
# Swaggerドキュメント生成
swag init -g cmd/api/main.go -o docs/swagger

# Swagger UIで確認
# http://localhost:8080/swagger/index.html
```

---

## 📚 ドキュメントナビゲーション

### プロジェクトドキュメント（`docs/`）

| ドキュメント | 説明 |
|------------|------|
| **[docs/product-requirements.md](docs/product-requirements.md)** | 製品要件、ユーザーストーリー、受け入れ基準 |
| **[docs/functional-design.md](docs/functional-design.md)** | 機能仕様、API設計、データモデル |
| **[docs/architecture.md](docs/architecture.md)** | システムアーキテクチャ、コンポーネント設計 |
| **[docs/development-guidelines.md](docs/development-guidelines.md)** | 開発ガイドライン、コーディング規約、ワークフロー |
| **[docs/repository-structure.md](docs/repository-structure.md)** | ディレクトリ構成、ファイル責務 |
| **[docs/glossary.md](docs/glossary.md)** | ドメイン用語、技術用語の定義 |

### コーディング規約（`.claude/skills/`）

| ドキュメント | 説明 |
|------------|------|
| **[.claude/skills/go-standards/SKILL.md](.claude/skills/go-standards/SKILL.md)** | Go言語コーディング規約 |
| **[.claude/skills/test-standards/SKILL.md](.claude/skills/test-standards/SKILL.md)** | テストコーディング規約 |
| **[.claude/skills/security-standards/SKILL.md](.claude/skills/security-standards/SKILL.md)** | セキュリティコーディング規約 |

### EDAF v1.0 設定

| ドキュメント | 用途 |
|------------|------|
| **[.claude/CLAUDE.md](.claude/CLAUDE.md)** | EDAF v1.0 エージェントシステム、7段階ゲートシステム詳細 |
| **[CHANGELOG.md](CHANGELOG.md)** | セマンティックバージョニング準拠の変更記録 |
| **[AGENTS.md](AGENTS.md)** | リポジトリガイドラインと規約 |

---

## ⚠️ トラブルシューティング

問題が発生した場合:
1. [CHANGELOG.md](CHANGELOG.md) で最新の変更を確認
2. [.claude/CLAUDE.md](.claude/CLAUDE.md) でエージェントシステムの使い方を確認
3. Docker ログを確認: `docker compose logs -f app`
4. GitHub Issuesで報告

---

## 🎓 学習リソース

### 外部リソース

- [Go by Example](https://gobyexample.com/)
- [クリーンアーキテクチャ完全に理解した](https://qiita.com/nrslib/items/a5f902c4defc83bd46b8)
- [Anthropic Claude API ドキュメント](https://docs.anthropic.com/)
- [OpenAI API ドキュメント](https://platform.openai.com/docs/)
- [PostgreSQL 公式ドキュメント](https://www.postgresql.org/docs/)

---

## 📄 ライセンス

このプロジェクトはMITライセンスの下で公開されています。
詳細は [LICENSE](LICENSE) を参照してください。

---

**ドキュメント最終更新:** 2026-01-09
**EDAF v1.0 7フェーズシステム移行完了:** 2026-01-09
