# リポジトリ構成

**対象**: catchup-feed-backend(Go 1.25.6 単一モジュール、module name: `catchup-feed`)
**最終更新**: 2026-08-15

ディレクトリとパッケージの責務を記述します。層の設計思想・依存ルール・技術選定の理由は [architecture.md](architecture.md) を参照してください。

---

## 目次

1. [全体像](#1-全体像)
2. [ルート直下](#2-ルート直下)
3. [`cmd/` — エントリポイント](#3-cmd--エントリポイント)
4. [`internal/` — アプリケーション本体](#4-internal--アプリケーション本体)
5. [`pkg/` — 公開パッケージ](#5-pkg--公開パッケージ)
6. [`deploy/` — 運用](#6-deploy--運用)
7. [`docs/` — ドキュメント](#7-docs--ドキュメント)
8. [配置と命名の規約](#8-配置と命名の規約)
9. [コード規模](#9-コード規模)

---

## 1. 全体像

```text
catchup-feed-backend/
├── cmd/                   # エントリポイント(composition root)
│   ├── server/            #   Pi 常駐: 管理 API + フィード配信
│   ├── worker/            #   Pi 常駐: クロール + ジョブ処理
│   ├── radio/             #   Mac 夜間バッチ: 番組生成
│   ├── hash-password/     #   管理者パスワードの bcrypt ハッシュ生成
│   └── crawl-once/        #   開発用の単発クロール
├── internal/              # アプリケーション本体(外部から import 不可)
│   ├── domain/entity/     #   [Domain]         エンティティ・不変条件
│   ├── repository/        #   [Port]           永続化インターフェース
│   ├── usecase/           #   [UseCase]        アプリケーションロジック
│   ├── handler/http/      #   [Presentation]   HTTP ハンドラ・ミドルウェア
│   ├── infra/             #   [Infrastructure] DB アダプタ・外部 API
│   ├── radio/             #   [radio]  番組生成パイプライン
│   ├── script/            #   [radio]  台本・クイズ生成
│   ├── tts/               #   [radio]  音声合成・ffmpeg
│   ├── feed/              #   [server+worker] RSS 生成・フィード配信
│   ├── jobs/              #   [worker+radio]  ジョブコンシューマ / 投入
│   ├── learning/          #   [server+radio]  SRS 純関数(共有コア)
│   ├── notify/            #   [worker] 管理者向けメール通知(SMTP)
│   ├── common/pagination/ #   共通: ページネーション
│   ├── pkg/               #   共通: 検索・バリデーション・設定ロード
│   ├── service/auth/      #   認証ポート
│   └── utils/text/        #   文字数カウント
├── pkg/                   # 外部 import 可の公開パッケージ
├── deploy/                # Pi / Mac へのデプロイ資材(運用スクリプトは deploy/scripts/)
├── docs/                  # 設計ドキュメント + Swagger 生成物
├── data/                  # ローカル実行時の mp3 / 書籍置き場(git 管理外)
├── compose.yml            # postgres / server / worker
├── Dockerfile             # マルチステージビルド
└── Makefile               # 開発・テスト・リントのタスク
```

`internal/` の各パッケージが、Clean Architecture の 4 層に属するもの(`[Domain]` 〜 `[Infrastructure]`)と、**層ではなく用途で切ったもの**に分かれる点が本リポジトリの構成上の特徴です。後者は角括弧に**利用するバイナリ**を書いてあります。`feed` は `cmd/server`(配信ハンドラ)と `cmd/worker`(`FEED_AUDIO_DIR` 等の設定読み取り)が使う **server 側のパッケージ**で、`cmd/radio` からは参照しません。判断根拠は [architecture.md §3.4](architecture.md#34-意図的な逸脱とその理由) にあります。

---

## 2. ルート直下

| ファイル | 内容 |
|---|---|
| `README.md` | 概要・アーキテクチャ・セットアップ・環境変数一覧 |
| `CLAUDE.md` | AI エージェント向けのリポジトリ固有ルール |
| `CHANGELOG.md` | 変更履歴 |
| `compose.yml` | 開発・本番共通のベース定義(`postgres` / `server` / `worker`)。本番 Pi では `deploy/compose.pi.yml` を override で重ねる |
| `Dockerfile` | 4 ステージ(`deps` → `dev` / `build` → Alpine ランタイム。ランタイムはダイジェスト固定) |
| `Makefile` | 開発タスク。CI と同じコマンドをローカルから実行できる |
| `.golangci.yml` | `errcheck` / `govet` / `staticcheck` / `unused` / `ineffassign` |
| `.air.toml` | ホットリロード設定(開発コンテナ) |
| `go.mod` / `go.sum` | 依存定義。直接依存は 14 パッケージのみ |

主要な Make ターゲット: `setup` / `dev-up` / `dev-down` / `dev-shell` / `build` / `test` / `test-unit` / `test-coverage` / `lint` / `lint-fix` / `fmt` / `swagger` / `swagger-host` / `admin-hash` / `db-reset` / `db-shell` / `logs` / `ci` / `clean`(一覧は `make help`)。`swagger-host` だけが Docker を使わずホストの Go で Swagger を生成する退避経路です(Docker 停止時用)。

---

## 3. `cmd/` — エントリポイント

いずれも依存の組み立てとライフサイクル管理に徹し、ロジックを持ちません。

### `cmd/server/`(561 行)

Pi 常駐の HTTP サーバー。2 つのリスナーを持ちます。

- **公開リスナー(:8080)** — 管理 API(JWT)+ 公開フィード配信(トークン認証)+ `/health` `/ready` `/live` + `/swagger/`
- **私的リスナー(:8081)** — tailnet バインド。私的フィードと書籍 PDF 配信。CORS / CSP / 認証を適用しない(C-5)

主な処理:

| 関数 | 責務 |
|---|---|
| `main` | 設定読み込み → DB 接続 → マイグレーション適用 → 2 リスナー起動 → graceful shutdown |
| `setupRoutes` | 各ハンドラパッケージの `Register` を呼びルートを登録。レート制限器を生成 |
| `applyMiddleware` | ミドルウェアチェーンを構成(CORS → RequestID → Recover → Logging → BodyLimit → CSP) |
| `startRateLimiterCleanup` | インメモリのレート制限エントリを定期的に掃除 |

依存の注入は構造体リテラルで行います(DI コンテナ不使用)。

```go
srcSvc := srcUC.Service{Repo: pgRepo.NewSourceRepo(database)}
artSvc := artUC.Service{Repo: pgRepo.NewArticleRepo(database)}
```

### `cmd/worker/`(393 行)

Pi 常駐のバックグラウンドワーカー。robfig/cron で 2 つの定期実行(毎時クロール / 古いメディアの掃除)を回しつつ、`jobs` テーブルのコンシューマを並行して動かします。

### `cmd/radio/`(185 行)

Mac の夜間バッチ(launchd 起動)。`internal/radio.Pipeline` に必要な実装を注入して 1 回実行し終了します。

### `cmd/hash-password/`(81 行) / `cmd/crawl-once/`(167 行)

前者は `ADMIN_PASSWORD_HASH` に設定する bcrypt ハッシュを標準入力から生成します(`make admin-hash`)。後者は開発用の単発クロールです。

---

## 4. `internal/` — アプリケーション本体

### 4.1 `internal/domain/entity/` — Domain 層(516 行)

エンティティと不変条件。**標準ライブラリ以外に依存せず**、ORM / DB タグを持ちません。

| ファイル | 内容 |
|---|---|
| `article.go` `source.go` `summary.go` | 記事・収集元・要約。`Source.Validate()` を持つ |
| `episode.go` `segment.go` | 番組とそのセグメント |
| `job.go` | ジョブ種別定数(`regenerate_feed` / `notify_episode` / `notify_error` / `cleanup_old_media` / `transcribe` / `book_ingest`)とペイロード構造体。json タグを持つ唯一のファイル(`jobs.payload` の JSONB 用) |
| `feed_token.go` | 配信トークン。`GenerateFeedToken()` / `HashFeedToken()`(SHA-256)と `IsRevoked()` |
| `feed_access_log.go` | アクセスログ |
| `subscriber.go` `viewer.go` | 友人・ダッシュボード閲覧者。`IsActive()` |
| `validation.go` | `ValidateURL` — スキーム検証とプライベート IP 帯の拒否(SSRF 対策の一次防御) |
| `errors.go` | `ValidationError` |

### 4.2 `internal/repository/` — Port 層(791 行)

永続化インターフェース **14 本**と、境界を越える構造体(`ArticleWithSource` / `ArticleSearchFilters` / `PendingReview` / `RadioArticle` 等)。実装は持ちません。

`article_repository.go` `book_admin_repository.go` `book_review_repository.go` `episode_repository.go` `feed_access_log_repository.go` `feed_token_repository.go` `job_repository.go` `learning_admin_repository.go` `learning_repository.go` `radio_article_repository.go` `source_repository.go` `subscriber_repository.go` `summary_repository.go` `viewer_repository.go`

### 4.3 `internal/usecase/` — UseCase 層(3,394 行)

| パッケージ | 行数 | 責務 |
|---|---|---|
| `fetch/` | 1,543 | クロール本体。RSS / YouTube / ポッドキャストの取り込み、本文抽出、要約、ニュースレターのリンク展開、期限切れ記事のスイープ。ポート 6 本を自パッケージに定義 |
| `book/` | 509 | 書籍 PDF のアップロード・一覧・削除(D-25) |
| `article/` | 334 | 記事 CRUD・検索・ページネーション |
| `viewer/` | 311 | 閲覧者アカウント管理(bcrypt 照合を含む) |
| `subscriber/` | 230 | 友人管理・配信トークンの発行/失効 |
| `source/` | 213 | 収集元 CRUD |
| `learning/` | 167 | 復習キューの取得・採点・リタイア |
| `accesslog/` | 87 | アクセスログ集計 |

各パッケージは `service.go`(本体)と `errors.go`(センチネルエラー)で構成します。`Service` はリポジトリインターフェースを公開フィールドで受け取ります。

### 4.4 `internal/handler/http/` — Presentation 層(5,720 行)

| パッケージ | 責務 |
|---|---|
| (直下) | `health.go`(`/health` `/ready` `/live`)、`middleware.go`(Recover / Logging / BodyLimit)、`timeout.go`、`validation.go` |
| `article/` `source/` | CRUD + 検索。`register.go` がルート登録、`dto.go` が入出力変換 |
| `subscriber/` | 友人管理 + トークン発行/失効(`tokens.go`) |
| `viewer/` | 閲覧者アカウント管理(D-27) |
| `learning/` | 復習キュー(`reviews.go`)・アイテム(`items.go`)・書籍(`books.go`) |
| `book/` | PDF アップロード/削除(`handlers.go`)、tailnet 限定のファイル配信(`private.go`) |
| `accesslog/` | アクセスログ参照 |
| `auth/` | JWT 発行・検証(`token.go` `middleware.go`)、HttpOnly Cookie(`cookie.go`)、管理者資格情報の検証(`provider.go` `validator.go`)、`GET /auth/me`(`me.go`) |
| `middleware/` | CORS(4 ファイル)、CSP、レート制限、`IPExtractor`(XFF 詐称対策) |
| `respond/` | JSON レスポンスとエラーのサニタイズ |
| `pathutil/` | パスパラメータの ID 抽出・正規化・ログ用の秘匿化 |
| `requestid/` | リクエスト ID の採番と context 伝播 |
| `responsewriter/` | ステータスコード記録用のラッパー |

ルーティングは各パッケージの `Register(mux, service, ...)` に集約し、`cmd/server` からはそれを呼ぶだけにしています。

### 4.5 `internal/infra/` — Infrastructure 層(5,703 行)

| パッケージ | 行数 | 責務 |
|---|---|---|
| `adapter/persistence/postgres/` | 2,936 | `repository` の 14 インターフェースを実装。1 リポジトリ 1 ファイル + `article_query_builder.go`(動的検索クエリ組み立て) |
| `summarizer/` | 1,207 | 要約 LLM。`chain.go` がフォールバック連鎖(Gemini → Groq → Ollama)、`gemini.go` `groq.go` `ollama.go` `gemini_video.go` が各プロバイダ、`noop.go` はキー未設定時 |
| `fetcher/` | 668 | HTTP 取得。`readability.go`(本文抽出)、`url_validation.go`(ホップごとの SSRF 検証)、`config.go`(サイズ・リダイレクト上限)、`useragent.go` |
| `worker/` | 424 | ワーカーの設定とヘルスチェック |
| `db/` | 377 | 接続(`open.go`)と冪等マイグレーション(`migrate.go`)。`seeds/sources.sql` に初期収集元 |
| `scraper/` | 91 | gofeed による RSS / Atom パース |

### 4.6 用途別パッケージ

層ではなく用途で切ったパッケージです。「利用」列は `go list` で確認した実際の import 元(`cmd/` 配下)です。

#### radio(Mac 夜間バッチ)専用

| パッケージ | 行数 | 主なファイルと責務 |
|---|---|---|
| `radio/` | 1,849 | `pipeline.go`(番組生成の全工程 + 必要な依存 10 本の interface 定義)、`transfer.go`(rsync 転送。`Transferer` / `RunFunc` で差し替え可能)、`bookreview.go`(書籍コーナー)、`weeklyreview.go`(週次振り返り)、`jingle.go`、`config.go` |
| `script/` | 1,440 | `generator.go`(台本生成)、`plan.go`(構成計画)、`quiz.go` `quizcorner.go`(クイズ)、`format.go`(番組の定型句を集約 — D-37)、`shownotes.go`、`prompts.go` + `prompts/`(テンプレート)、`bookreview.go` `weeklyreview.go` |
| `tts/` | 771 | `voicevox.go`(HTTP API 直叩き)、`ffmpeg.go`(結合・loudnorm)、`silence.go`(無音生成)、`wav.go`、`sentence.go`(文分割)、`jingle.go` + `assets/` |

#### 複数バイナリが使うもの

| パッケージ | 行数 | 利用 | 主なファイルと責務 |
|---|---|---|---|
| `feed/` | 817 | **server** + worker | `server.go`(公開/私的フィードのハンドラ・トークン検証・mp3 配信)、`rss.go`(XML 生成)、`artwork.go` + `assets/`、`config.go`。配信ハンドラは `cmd/server` が使い、worker は `LoadConfig()` から `FEED_AUDIO_DIR` / `FEED_PRIVATE_BASE_URL` を読むためだけに参照する。**radio からは参照しない** |
| `jobs/` | 666 | worker + radio | `consumer.go`(kind ごとのディスパッチ・`ClaimNext` / `RequeueRunning`)、`regenerate_feed.go` `notify_episode.go` `notify_error.go` `cleanup.go`。コンシューマを回すのは worker、radio は投入側(`NewNotifyErrorPayload`)としてのみ使う |
| `learning/` | 433 | server + radio | `transition.go`(SRS 遷移の純関数)、`date.go`(JST 放送日)、`item.go`、`weekly.go`、`config.go`(ラダー既定値 `1,7,30`)。採点 API と radio が同じ遷移関数を共有する。**外側に依存しない** |
| `notify/` | 324 | worker | `notify.go`(`Destination` インターフェースと `Message`)、`email.go`(唯一の実装 = 管理者宛メール)、`smtp.go`(SMTP クライアント)、`config.go`(`SMTP_*` / `NOTIFY_ERROR_EMAIL_TO`)。Webhook 系チャネルは D-29 で廃止済み |

### 4.7 共通パッケージ

| パッケージ | 行数 | 責務 |
|---|---|---|
| `common/pagination/` | 303 | ページネーションのパラメータ解析・メタデータ生成・戦略切り替え |
| `pkg/config/` | 685 | 設定値のロード・検証と、警告付きのロード結果表現 |
| `pkg/validation/` | 140 | クエリパラメータのパース |
| `pkg/search/` | 130 | 検索キーワードのエスケープと正規化 |
| `service/auth/` | 44 | `AuthProvider` インターフェース(実装は `handler/http/auth/provider.go`) |
| `utils/text/` | 22 | 文字数カウント(要約の文字数上限チェック用) |

> **注**: 設定まわりは `internal/pkg/config`(ロード結果と警告)と `pkg/config`(環境変数ヘルパ)の 2 箇所です。かつては `internal/config`(`config/security.yaml` の YAML 読み込み、108 行)もありましたが、**import 元がゼロ**で 2026-08-15 に削除しました(D-44)。

---

## 5. `pkg/` — 公開パッケージ

外部から import 可能な位置に置くパッケージ(計 770 行)。現状 2 つのみです。

| パッケージ | 内容 |
|---|---|
| `pkg/config/` | `env.go`(環境変数の型付き読み取り)、`duration.go`、`csp.go` |
| `pkg/security/csp/` | CSP ポリシービルダー。`StrictPolicy()` と `SwaggerUIPolicy()` |

---

## 6. `deploy/` — 運用

| パス | 内容 |
|---|---|
| `compose.pi.yml` | 本番 Pi 用の override(`compose.yml` に重ねる) |
| `systemd/pulse.service` | Pi 側のサービス定義 |
| `launchd/*.plist` | Mac 側の定期実行(radio / transcribe / backup / morningcheck) |
| `scripts/` | `radio-run.sh` `transcribe-run.sh` `pi-health-check.sh` `morning-check.sh` `backup-pulse-db.sh` `alert-mail.sh` |
| `cloudflared/config.example.yml` | Cloudflare Tunnel の設定例 |
| `pi.md` / `mac.md` / `ai.md` / `README.md` | ホスト別の手順書 |
| `env.pi.example` / `env.mac.example` | ホスト別の環境変数テンプレート |

**運用スクリプトは `deploy/scripts/` だけが正です**。ルート直下にあった初代 EDAF 期の `scripts/` と `config/` は、Makefile・CI・本体コードからの参照がいずれもゼロだったため 2026-08-15 に削除しました(D-44)。

- **`scripts/`** — バックアップ・ヘルスチェック・ディスク監視・Docker 掃除・マルチアーキビルド・フィード診断。機能はすべて `deploy/scripts/` と `docker.yml` に移っている
- **`config/cron/` `config/logrotate/`** — 前者は**現行に存在しない Prometheus の掃除 cron**、後者はローテート対象のログの書き手(`scripts/lib/email-functions.sh`)ごと消えた。pulse で必要な logrotate は `pulse-health-check` のみ(`deploy/pi.md` 9章)
- **`config/security.yaml` `config/environments/` と `internal/config/`** — `security.yaml` は `/metrics` を公開エンドポイントに、認証を `basic` に指定しており現行(D-22 の JWT + HttpOnly クッキー)と食い違う。`environments/` は 3 環境 + Kubernetes + AWS Secrets Manager 前提で単一ユーザー右サイズの pulse とは別物。これらを読む `internal/config`(108 行)も **import 元がゼロ**だったため同時に削除した。env の正は `.env.example` と `deploy/env.pi.example` / `env.mac.example`

Pi 実機側の残骸(systemd unit・logrotate・旧 cron・旧バックアップ)の撤去状況は `deploy/legacy-shutdown.md` 8章が正です。内容が必要になったら git 履歴を参照してください。

---

## 7. `docs/` — ドキュメント

ルート直下にあったシェルベースの通知テストと共有フィクスチャ(`tests/`)は、現行の `internal/notify/` と無関係な旧メール基盤のテストだったため 2026-08-15 に削除しました(D-44)。Go のテストの配置規約は §8 が正です。

### `docs/`

| ファイル | 内容 | 種別 |
|---|---|---|
| `architecture.md` | 層構成・依存ルール・データフロー・セキュリティ・技術選定 | 手書き |
| `repository-structure.md` | 本ドキュメント | 手書き |
| `swagger.json` `swagger.yaml` `docs.go` | API 仕様 | `make swagger`(Docker)/ `make swagger-host`(ホスト)の生成物。gitignore 済みだが `cmd/server` が `docs.go` をブランクインポートするため、clone 後に1度生成しないと `./...` のビルドが通らない |

初代 catchup-feed(EDAF 体制期)の `development-guidelines.md` / `functional-design.md` /
`product-requirements.md` / `glossary.md` は 2026-08-13 に削除しました。gRPC・サーキット
ブレーカー・Prometheus・OpenAI/Claude 要約・RBAC など**現行に存在しない機能を現行として
記述しており**、放置すると読み手を誤らせるためです。内容が必要になったら git 履歴を参照
してください。コーディング規約は本ファイル §8 と `CLAUDE.md`、機能仕様は `/swagger/` と
親リポジトリの設計書が引き継いでいます。

> Phase 別の設計と決定ログ(D-xx / C-xx)は親リポジトリの `docs/` が正です。食い違う場合は設計書を優先してください。

### `.github/workflows/`

| ファイル | 内容 |
|---|---|
| `ci.yml` | 6 ジョブ。**Test**(pgvector サービスコンテナ → `go mod verify` → Swagger 生成 → `go test -race` → Codecov)/ **Lint**(Swagger 生成 → golangci-lint v2.12.2)/ **Shell Script Lint**(shellcheck v0.11.0 で `deploy/scripts/*.sh` を `-x` 付き検査)/ **Build**(server / worker のビルドとバイナリサイズ表示)/ **Security Scan**(Swagger 生成 → 型検査(`go build ./...`)→ gosec → SARIF)/ **Dependency Vulnerability Scan**(Swagger 生成 → govulncheck。終了コード 0/3 は緑、それ以外はスキャン不成立として赤 → SARIF)。Swagger 生成は Go を使う全ジョブに必要(未生成だと `cmd/server` が型検査を通らず、gosec は SSA 解析を、govulncheck は解析自体をスキップする) |
| `docker.yml` | QEMU + buildx によるマルチアーキイメージのビルド(Pi の arm64 向け) |

**セキュリティ系2ジョブの赤/緑の契約は 2026-08-15 に変わりました**。以前は gosec の `-no-fail` と各ステップの `continue-on-error` により**何が起きても緑**でしたが、現在は「**検出では緑、スキャンが不成立なら赤**」です。指摘・脆弱性の検出で個人開発の CI を止めない方針は変えず、gosec が SSA 解析を黙って飛ばす条件(`go build ./...` が通らない)と、govulncheck が解析自体に失敗した場合(終了コード 0 / 3 以外)だけを落とします。**緑を「スキャンが走って問題なし」と読んでよいのはこの契約があるため**で、これが無かった間は「動いていないのに緑」を実際に見逃していました。

ただし **SARIF アップロードの失敗は今も緑**です(両ジョブの Upload SARIF ステップには `continue-on-error: true` を残しています)。Code scanning が有効でない環境でも CI を止めないためで、赤くするのは**解析が成立しなかったとき**だけ、という切り分けです。

---

## 8. 配置と命名の規約

| 対象 | 規約 |
|---|---|
| テストファイル | 対象と同じディレクトリに `*_test.go`。table-driven + testify |
| DB を使うテスト | `*_integration_test.go` として `internal/infra/db/` に配置 |
| 外部ネットワークを使うテスト | `//go:build integration` タグを付与 |
| ハンドラのルート登録 | 各パッケージの `register.go` に `Register(mux, service, ...)` を実装し、`cmd/server` はそれを呼ぶだけ |
| DTO | ハンドラパッケージの `dto.go` に集約 |
| ユースケースのエラー | `errors.go` にセンチネルエラーを定義し、ハンドラ側で `errors.Is` により HTTP ステータスへ写像 |
| リポジトリ実装 | 1 インターフェース 1 ファイル(`*_repo.go`) |
| モック | 生成ツールを使わず、テストファイル内に手書き(interface が小さいため) |
| 設計判断 | コメントに決定ログ番号(D-xx / C-xx / §x.x)を残し、設計書と対応付ける |

---

## 9. コード規模

2026-08-15 時点の実測です。**計測範囲は `internal/` + `pkg/` + `cmd/` 配下の `*.go`** で、「行数」列はテスト(`*_test.go`)を除いた値、最下行の「テスト」だけが `*_test.go` を数えた値です。再現コマンド:

```bash
find internal pkg cmd -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l   # 本体合計
find internal pkg cmd -name '*_test.go' | wc -l                                 # テストファイル数
find internal pkg cmd -name '*_test.go' -exec cat {} + | wc -l                   # テスト行数
```

| 区分 | パッケージ | 行数 |
|---|---|---|
| Presentation | `internal/handler` | 5,720 |
| Infrastructure | `internal/infra` | 5,703 |
| UseCase | `internal/usecase` | 3,394 |
| Port | `internal/repository` | 791 |
| Domain | `internal/domain/entity` | 516 |
| 用途別(radio) | `internal/radio` | 1,849 |
| 用途別(radio) | `internal/script` | 1,440 |
| 用途別(server + worker) | `internal/feed` | 817 |
| 用途別(radio) | `internal/tts` | 771 |
| 用途別(worker + radio) | `internal/jobs` | 666 |
| 用途別(server + radio) | `internal/learning` | 433 |
| 用途別(worker) | `internal/notify` | 324 |
| 共通 | `internal/pkg` `common` `service` `utils` | 1,324 |
| 公開 | `pkg/` | 770 |
| エントリポイント | `cmd/` | 1,387 |
| **本体合計** | | **25,913** |
| テスト | 161 ファイル | 47,930 |

interface 定義は 47 本、テスト内のモック・スタブは 65 個です。
