# catchup-feed

**RSS/Atomフィードを自動クロールし、AIで要約を生成するバックエンドシステム**

---

## 📋 プロジェクト概要

**catchup-feed** は、RSS/Atomフィードから記事を自動収集し、Claude/OpenAI APIを使用してAI要約を生成、REST APIで提供するバックエンドシステムです。

### 開発の背景・目的

日々大量に発信される技術記事やニュースを効率的にキャッチアップするため、AIによる自動要約機能を持つフィードリーダーのバックエンドとして開発しました。

### 主な特徴

- **クリーンアーキテクチャ採用**: 保守性・テスタビリティを重視した設計
- **AI要約機能**: Claude/OpenAI APIによる記事の自動要約生成
- **コンテンツ強化**: RSSの要約のみの記事は元記事から全文取得してAI要約品質を向上
- **マルチチャネル通知**: Discord/Slack連携（拡張可能な設計）
- **本番運用品質**: サーキットブレーカー、レート制限、Prometheusメトリクス対応
- **実稼働中**: Raspberry Pi 5 + Cloudflare Tunnelで本番運用（[デモサイト](#-本番環境)）

---

## 🛠️ 使用技術

### バックエンド

| カテゴリ | 技術 |
|---------|------|
| **言語** | Go 1.25.4 |
| **データベース** | PostgreSQL 16 / SQLite（テスト用） |
| **HTTPルーター** | 標準ライブラリ（net/http） |
| **認証** | JWT（golang-jwt/jwt/v5） |
| **AI API** | Anthropic Claude（Sonnet 4.5） / OpenAI（GPT-4o-mini） |
| **RSS解析** | mmcdole/gofeed |
| **スケジューラ** | robfig/cron/v3 |
| **監視** | Prometheus / Grafana |
| **ドキュメント** | Swagger（swaggo/swag） |

### 開発環境・ツール

| カテゴリ | 技術 |
|---------|------|
| **コンテナ** | Docker / Docker Compose |
| **CI/CD** | GitHub Actions |
| **静的解析** | golangci-lint / go vet |
| **テスト** | 標準testing / testify |

---

## 🏗️ アーキテクチャ

### クリーンアーキテクチャ

```
┌────────────────────────────────┐
│ プレゼンテーション層            │ ← cmd/api, internal/handler/http
│ （HTTPハンドラー）              │
├────────────────────────────────┤
│ ユースケース層                  │ ← internal/usecase
│ （ビジネスロジック）            │
├────────────────────────────────┤
│ ドメイン層                      │ ← internal/domain/entity
│ （エンティティ）                │
├────────────────────────────────┤
│ インフラストラクチャ層          │ ← internal/infra
│ （DB、外部API）                 │
└────────────────────────────────┘
```

**依存方向**: 外側 → 内側（プレゼンテーション → ユースケース → ドメイン）

### 設計原則

1. **依存性逆転の原則**: インターフェースを活用し、外部依存を抽象化
2. **ドメイン層の独立性**: 外部ライブラリへの依存を排除
3. **単一責任の原則**: 各ユースケースは1つの責務のみ担当
4. **テスタビリティ**: モックを使った単体テストが容易な設計

---

## 📁 ディレクトリ構成

```
catchup-feed/
├── cmd/
│   ├── api/                  # APIサーバー（ポート8080）
│   └── worker/               # バッチクローラー（定期実行）
├── internal/
│   ├── config/               # 設定管理
│   ├── domain/
│   │   └── entity/           # ドメインエンティティ（Article, Source, User）
│   ├── repository/           # リポジトリインターフェース
│   ├── service/
│   │   └── auth/             # 認証サービス
│   ├── usecase/              # ビジネスロジック層
│   │   ├── article/          # 記事ユースケース
│   │   ├── source/           # ソースユースケース
│   │   ├── fetch/            # フィード取得・要約生成
│   │   └── notify/           # マルチチャネル通知
│   ├── handler/http/         # HTTPハンドラー
│   │   ├── auth/             # 認証ハンドラー・JWT検証
│   │   ├── article/          # 記事ハンドラー
│   │   ├── source/           # ソースハンドラー
│   │   └── middleware/       # ミドルウェア（CORS、レート制限等）
│   ├── infra/
│   │   ├── adapter/          # 永続化アダプタ（SQLite実装）
│   │   ├── db/               # データベース接続・マイグレーション
│   │   ├── summarizer/       # 要約エンジン（Claude, OpenAI）
│   │   ├── scraper/          # RSS/Atom解析
│   │   ├── fetcher/          # コンテンツ取得（Readability）
│   │   └── notifier/         # 通知（Discord, Slack）
│   ├── observability/        # 監視（ロギング、メトリクス、トレーシング）
│   ├── resilience/           # 耐障害性（サーキットブレーカー、リトライ）
│   └── pkg/                  # 共通パッケージ（バリデーション等）
├── config/                   # 設定ファイル（Prometheus、Grafana等）
├── pkg/                      # 公開パッケージ（レート制限、セキュリティ）
├── scripts/                  # 運用スクリプト（バックアップ、ヘルスチェック）
├── tests/                    # テスト（E2E、統合、パフォーマンス）
├── docs/                     # プロジェクトドキュメント
└── .claude/                  # Claude Code設定
```

---

## 📱 主要機能

### 1. フィード自動収集・要約生成

- 登録されたRSS/Atomフィードを定期クロール（デフォルト: 毎日5:30 AM）
- 並列処理によるフィード取得の最適化
- URL重複検知による記事の重複防止
- Claude/OpenAI APIによる自動要約生成

### 2. コンテンツ強化機能

RSSフィードの内容が不十分な場合（要約のみの記事など）、元記事からフルテキストを自動取得してAI要約の品質を向上させる機能を実装しています。

**技術的なポイント**:
- Mozilla Readabilityアルゴリズムで記事本文を抽出
- SSRF防止: プライベートIPアクセスをブロック
- サイズ制限・タイムアウト・リダイレクト制限によるセキュリティ対策

### 3. 認証・認可

- JWT認証によるセキュアなAPI
- ロールベースアクセス制御（Admin / Viewer）
- 強力なパスワードポリシー（12文字以上、弱いパターン検出）

### 4. マルチチャネル通知

拡張可能な設計の通知システムを実装しています。

```
┌─────────────────────────────────────────┐
│ 通知サービス                             │
│   ├─ ゴルーチンプール（同時実行数制御）    │
│   └─ チャネル別ディスパッチ               │
└─────────────────────────────────────────┘
          │
    ┌─────┴─────┐
    ▼           ▼
┌─────────┐ ┌─────────┐
│ Discord │ │ Slack   │ （拡張可能）
├─────────┤ ├─────────┤
│サーキット│ │サーキット│
│ブレーカー│ │ブレーカー│
│レート制限│ │レート制限│
└─────────┘ └─────────┘
```

### 5. 監視・可観測性

- Prometheusメトリクス: 成功率、レイテンシ、レート制限ヒット数等
- ヘルスチェックエンドポイント
- 構造化ロギング

---

## 🚀 クイックスタート

### 前提条件

- Docker / Docker Compose
- Claude または OpenAI のAPIキー

### セットアップ

```bash
# 1. 環境変数を設定
cp .env.example .env
# .env を編集し、APIキー等を設定

# 2. 開発環境を起動
make setup

# 3. 開発コンテナに入る
make dev-shell

# 4. テスト実行
go test ./...

# 5. 静的解析
golangci-lint run
```

### アクセス

| サービス | URL | 説明 |
|---------|-----|------|
| API | http://localhost:8080 | REST API |
| Swagger UI | http://localhost:8080/swagger/index.html | APIドキュメント |
| Prometheus | http://localhost:9090 | メトリクス |
| Grafana | http://localhost:3000 | ダッシュボード |

---

## 📡 API概要

### 認証

```bash
# トークン取得
curl -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your-password"}'
```

### 主要エンドポイント

| メソッド | エンドポイント | 説明 |
|---------|---------------|------|
| POST | `/auth/token` | 認証トークン取得 |
| GET | `/sources` | フィードソース一覧 |
| POST | `/sources` | フィードソース登録 |
| GET | `/articles` | 記事一覧（要約付き） |
| GET | `/health` | ヘルスチェック |
| GET | `/metrics` | Prometheusメトリクス |

詳細は [Swagger UI](http://localhost:8080/swagger/index.html) を参照してください。

---

## 🔧 設定

### 必須環境変数

| 項目 | 説明 |
|------|------|
| `DATABASE_URL` | PostgreSQL接続文字列 |
| `JWT_SECRET` | JWT署名用秘密鍵（32文字以上） |
| `ANTHROPIC_API_KEY` または `OPENAI_API_KEY` | AI APIキー |
| `ADMIN_USER_PASSWORD` | 管理者パスワード |

### 要約エンジンの選択

| エンジン | 推奨用途 | 必要な環境変数 |
|----------|----------|---------------|
| `openai` | 開発環境（コスト優先） | `OPENAI_API_KEY` |
| `claude` | 本番環境（品質優先） | `ANTHROPIC_API_KEY` |

詳細な設定項目は `.env.example` を参照してください。

---

## 🎯 開発ガイドライン

### コーディング規約

- **クリーンアーキテクチャ準拠**: 依存方向を厳守（外側 → 内側）
- **ドメイン層の独立性**: 外部依存を持たない（標準ライブラリのみ）
- **テストカバレッジ**: 70%以上を維持
- **静的解析**: go fmt / goimports / go vet / golangci-lint

### エラーハンドリング

```go
// コンテキスト情報を付与
if err != nil {
    return fmt.Errorf("failed to create article: %w", err)
}
```

### ブランチ命名規則

- `feature/XXX` - 新機能
- `fix/XXX` - バグ修正
- `hotfix/XXX` - 緊急修正

---

## 🌐 本番環境

### デモサイト

本プロジェクトは**実際に稼働しているサービス**です。

| 環境 | URL |
|------|-----|
| **フロントエンド** | [pulse.catchup-feed.com](https://pulse.catchup-feed.com) |
| **バックエンドAPI** | [catchup.catchup-feed.com](https://catchup.catchup-feed.com) |

### インフラ構成

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         catchup-feed.com                                │
│                      (Cloudflare DNS Zone)                              │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌───────────────────────┐         ┌───────────────────────┐           │
│  │pulse.catchup-feed.com │         │catchup.catchup-feed.com│           │
│  │       (CNAME)         │         │       (CNAME)          │           │
│  │          ↓            │         │          ↓             │           │
│  │    Vercel Edge        │         │   Cloudflare Tunnel    │           │
│  │    (Next.js SSR)      │         │          ↓             │           │
│  │                       │         │    Raspberry Pi 5      │           │
│  │ catchup-feed-frontend │←───────→│ catchup-feed-backend   │           │
│  │      (Frontend)       │   API   │   (Go API + Claude)    │           │
│  └───────────────────────┘         └───────────────────────┘           │
│                                              │                          │
│                                              ▼                          │
│                                     ┌─────────────────┐                 │
│                                     │   PostgreSQL    │                 │
│                                     └─────────────────┘                 │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

### 技術的なポイント

| 項目 | 内容 |
|------|------|
| **ホスティング** | Raspberry Pi 5（8GB）でGoバックエンド + PostgreSQLを運用 |
| **セキュア公開** | Cloudflare Tunnelでポート開放なしにインターネット公開 |
| **フロントエンド** | Vercel Edge Networkによるグローバルな CDN配信 |
| **SSL/TLS** | Cloudflare / Vercelによる自動証明書管理 |
| **CI/CD** | GitHub Actionsによる自動テスト・デプロイ |

### なぜRaspberry Pi 5？

- **低コスト運用**: クラウドサービスの月額費用を抑えながら本番運用
- **学習目的**: インフラ構築からデプロイまで一貫した経験
- **実用性の証明**: 軽量なGoバックエンドは低スペック環境でも十分なパフォーマンス

---

## 📚 ドキュメント

| ドキュメント | 説明 |
|------------|------|
| [docs/architecture.md](docs/architecture.md) | システムアーキテクチャ |
| [docs/development-guidelines.md](docs/development-guidelines.md) | 開発ガイドライン |
| [docs/functional-design.md](docs/functional-design.md) | 機能設計 |
| [CHANGELOG.md](CHANGELOG.md) | 変更履歴 |

---

## 📄 ライセンス

MITライセンス - 詳細は [LICENSE](LICENSE) を参照

---

**最終更新**: 2026-01-18
