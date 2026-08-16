# catchup-feed — backend

> 毎朝10〜15分の音声ラジオ番組をポッドキャストアプリに配信し、「理解の定着」を最適化する個人向け学習システムのバックエンド。

現行の `catchup-feed` は、RSS を要約して REST API / Discord に流す news aggregator だった**初代 catchup-feed** を作り直した後継です。初代は「配信された記事数」を最適化していましたが、Discord・Slack に流れる要約をすべて読むことは負荷が大きいものでした。そこで本システムが最適化するのは **理解の定着** です。可処分時間が細切れで手も目も塞がっている時間帯(移動中・家事中)に消化できるよう、応答形態を **音声** に変えました。RSS を要約し、毎朝ラジオ番組(mp3)を生成し、ポッドキャストアプリ経由で本人と友人に届け、フィードバックを得ます。

このリポジトリは catchup-feed の Go バックエンドです。フロントエンド(ダッシュボード)は [catchup-feed-frontend](https://github.com/Tsuchiya2/catchup-feed-frontend)、文字起こし・書籍 RAG は [catchup-feed-ai](https://github.com/Tsuchiya2/catchup-feed-ai) にあります。

---

## 出力されるもの

このリポジトリの成果物は API ではなく**音声番組**です。`cmd/radio` が 04:30 JST に起動して mp3 を生成できた日は、`cmd/server` がそれを RSS で配信し、購読中のポッドキャストアプリに届きます(下は AntennaPod での受信・再生。ショーノートに並ぶリンクは、その日番組で紹介した記事です)。新しい要約済み記事も期日到来の復習項目もアクティブな書籍も無い日は、番組を作らずスキップします(D-1)。

<img src="docs/images/episode-in-podcast-app.gif" alt="ポッドキャストアプリで catchup-feed のエピソードを再生している画面。ショーノートに当日紹介した記事のリンクが並んでいる" height="480">

---

## 設計原則

catchup-feed は**単一ユーザー**が**ゼロ円**で運用する自宅ホスティングを前提に「右サイズ」で作られています。初代 catchup-feed が抱えていた gRPC・サーキットブレーカー・Prometheus・Grafana・OpenTelemetry・マイクロサービス分割・OpenAI/Claude 依存はすべて**削除済み**です。

- **単一ユーザー右サイズ** — 冗長化・可観測性基盤・内部 RPC を持たない。プロセス間連携は PostgreSQL のジョブテーブルのみ。
- **ゼロ円運用** — 要約は無料枠 API → ローカル LLM のフォールバック連鎖。有料 API・有料 SaaS を使わない。
- **縮退許容** — 「壊れない」より「壊れても翌日勝手に戻る」。Mac 不在 → その日のエピソードは欠番、無料 API 全滅 → Ollama にフォールバック、VOICEVOX 障害 → 当日スキップ+通知。
- **プライバシー分界** — 無料クラウド API に流してよいのは公開記事とその要約のみ。書籍・私的データはローカル LLM(Ollama)のみで処理する。

---

## アーキテクチャ

Go 1.25.6 の単一モジュールで、**3つのバイナリ**を持ちます。内部 HTTP/RPC はなく、`server` / `worker` / `radio` はすべて **PostgreSQL 経由**(`jobs` テーブル+状態テーブル)で連携します。

| バイナリ | 配置 | 役割 |
|---|---|---|
| `cmd/server` | Pi 5(常駐) | 公開フィード配信(`/feeds/{token}/*`、トークン認証)+ 管理 API(JWT。書籍 PDF のアップロード/一覧/削除 `/books` を含む、D-25)+ tailnet 限定の私的フィード・書籍配信(`/private/*`)。起動時に冪等マイグレーションを自動適用。 |
| `cmd/worker` | Pi 5(常駐) | robfig/cron で毎時クロール → 本文抽出 → 要約 → DB 更新。`jobs` テーブルのコンシューマとして `regenerate_feed` / `notify_episode` / `notify_error` / `cleanup_old_media` を処理。 |
| `cmd/radio` | M3 Mac(夜間バッチ) | 記事選定 → LLM 台本生成 → VOICEVOX で音声合成 → ffmpeg で結合・mp3 化 → rsync で Pi へ転送 → `episodes`/`segments` を登録。Phase 3 のクイズ・書籍コーナーも同一ランで生成。 |

補助バイナリ: `cmd/hash-password`(管理者パスワードの bcrypt ハッシュ生成)、`cmd/crawl-once`(開発用の単発クロール)。

### ホスト配置

```text
┌──────────── Raspberry Pi 5(常時稼働)──────────────┐
│  server  : 公開フィード配信 / 管理 API / 私的フィード  │
│  worker  : クロール・要約・通知(cron 常駐)          │
│  PostgreSQL + mp3 アーカイブ(episodes/)             │
└──────────────────────────────────────────────────┘
          ▲ Tailscale(rsync / PostgreSQL 接続)
┌──────────── M3 MacBook Pro(夜間バッチ)────────────┐
│  radio   : 台本構成 → VOICEVOX → ffmpeg 結合         │
│  VOICEVOX Engine(現行話者: No.7 アナウンス)         │
│  Ollama(要約・書籍のローカル LLM フォールバック)     │
└──────────────────────────────────────────────────┘

公開経路: Cloudflare Tunnel → Pi server
  - pulse.catchup-feed.com         → Next.js ダッシュボード(JWT 保護)
  - radio.catchup-feed.com/feeds/* → 公開フィード(トークン認証)
私的経路: Tailscale(tailnet 内のみ、認証は物理境界)
  - pi.tailnet:8081/private/feed.xml
```

### 日次フロー

```text
[worker/Pi]  毎時       : クロール → articles 挿入 → 要約 → summaries 更新
[radio/Mac]  04:30 JST  : 当日分エピソード生成
   1. 対象記事選定(前回エピソード以降の要約済み記事)
   2. 台本生成(LLM): セグメントごとの読み上げ原稿(番組の言い回し・定型句は `internal/script/format.go` に集約 — D-37)
   3. VOICEVOX でセグメント別に合成 → 前後にオープニング/エンディングジングルを付けて
      ffmpeg で結合・mp3 化(64kbps mono。ジングルは `internal/tts/assets/` に go:embed — D-36)
   4. rsync で Pi の episodes/ へ転送、episodes / segments を INSERT
      → jobs に regenerate_feed / notify_episode を積む
   5. 私的版(復習クイズ・週次振り返り・書籍コーナー入り)を best-effort で追加生成
[worker/Pi]  ジョブ検知  : 管理者宛メールで新着エピソードを通知(D-29)
```

Mac が閉じていた日はエピソードが生成されないだけで、システムは壊れません(翌日に持ち越し)。

### レイヤー構成

管理 API(`cmd/server`)は Clean Architecture のレイヤーに沿って分割し、依存は常に内向きです。永続化のインターフェースを内側(`internal/repository`)に置き、PostgreSQL アダプタが外側からそれを実装します(依存性逆転)。

```text
外 ─────────────────────────────────────────────────→ 内
handler/http/*  →  usecase/*  →  repository/*(interface)  →  domain/entity
                                        ▲
infra/adapter/persistence/postgres ─────┘ 実装を cmd/ で注入
```

| ディレクトリ | 層 | 責務 |
|---|---|---|
| `internal/domain/entity` | Domain | エンティティ・不変条件・SSRF を含む URL 検証。**標準ライブラリ以外に依存しない**(ORM/DB タグを持たない) |
| `internal/repository` | Port | 永続化インターフェース(14 本)。実装は持たない |
| `internal/usecase/*` | UseCase | アプリケーションロジック(記事・ソース・友人・閲覧者・学習・書籍・クロール) |
| `internal/handler/http/*` | Presentation | ルーティング・DTO・JWT 認証・CORS/CSP・レート制限 |
| `internal/infra/*` | Infrastructure | PostgreSQL アダプタ・HTTP フェッチャ・要約 LLM クライアント |

残りは層ではなく**用途別のパッケージ**に切っています。外部依存は「必要なメソッドだけを利用側で定義する」Go 流のポート(consumer-side interface)で抽象化します。「利用」列は `go list` で確認した実際の import 元です。

| ディレクトリ | 利用 | 責務 |
|---|---|---|
| `internal/radio` | radio | 番組生成パイプライン。必要な依存を 10 本の interface として自パッケージに定義(`ArticleSource` / `EpisodeStore` / `Synthesizer` / `Transferer` 等)し、`repository` や `tts` の具象型には依存しない |
| `internal/script` | radio | LLM 台本生成・クイズ生成・番組の定型句(D-37) |
| `internal/tts` | radio | VOICEVOX 合成・無音生成・ffmpeg 結合 |
| `internal/feed` | **server** + worker | RSS XML 生成と公開/私的フィードの配信ハンドラ。配信は `cmd/server` の担当で、worker は `FEED_AUDIO_DIR`(mp3 の掃除先)などの設定を読むためだけに参照する。**radio からは参照しない** |
| `internal/jobs` | worker + radio | `jobs` テーブルのコンシューマ(`regenerate_feed` / `notify_episode` / `notify_error` / `cleanup_old_media`)。radio は投入側としてのみ使う |
| `internal/learning` | server + radio | SRS の遷移純関数・JST 放送日ヘルパ。採点 API と radio の共有コアのため**外側に一切依存させない** |
| `internal/notify` | worker | 管理者向けメール通知(SMTP)。`Destination` インターフェースの実装はメール 1 種のみ(D-29) |

依存ルールは `go list` で検証しています。

- `handler` → `infra` の直接参照: **0 件**(永続化は必ず `usecase` 経由)
- `usecase` / `domain` / `repository` → `handler` / `infra` の逆流: **0 件**
- `domain/entity` の外部依存: **0 件**(標準ライブラリのみ)

**意図的な逸脱**: `internal/feed` は配信ハンドラを持ちながら `handler/` の外に、`internal/tts` と `internal/notify` は実体がインフラでありながら `infra/` の外にあります。「1 バイナリの責務を、そのバイナリだけが使うパッケージにまとめる」ことを層の見た目より優先した結果です。判断の背景は [docs/architecture.md](docs/architecture.md) に記載しています。

---

## 技術スタック

- **言語 / ランタイム**: Go 1.25.6(単一モジュール、標準ライブラリの `net/http` ルーター — 外部ルーター依存なし)。バージョンは `.go-version` / `go.mod` / `Dockerfile` / CI の `GO_VERSION` で統一。
- **データベース**: PostgreSQL(ドライバは pgx/v5)。マイグレーションは `cmd/server` 起動時に冪等 SQL を自動適用。
- **認証**: 管理 API は JWT(golang-jwt/v5)+ 単一管理者(環境変数 + bcrypt ハッシュ)。フィード配信は URL 埋め込みの不透明トークン(`crypto/rand` 32byte → base64url、DB には SHA-256 ハッシュのみ保存)。
- **クローラー**: gofeed(RSS/Atom パース)+ go-readability(本文抽出)。リダイレクトごとに SSRF ガード。
- **要約 LLM(フォールバック連鎖)**: Gemini → Groq → Ollama。無料枠 API が全滅してもローカル(Ollama)で縮退継続。API キー未設定のプロバイダは連鎖から自動除外。
- **音声合成 (TTS)**: VOICEVOX(HTTP API を直叩き。話者はコード既定 3 = ずんだもん、実運用は `VOICEVOX_SPEAKER=30`(No.7 アナウンス)— D-2)。
- **音声処理**: ffmpeg(ジングル結合・loudnorm・mp3 エンコード)、rsync(Pi への転送)を `exec.Command` で呼び出し。ジングル mp3 は VOICEVOX 出力の実測 WAV フォーマットへ実行時デコードしてから concat する(D-36)。
- **スケジューラ**: robfig/cron(worker)、launchd(radio の夜間起動)。
- **学習ループ(Phase 3)**: `internal/learning/` に spaced repetition(SRS)の間隔ラダー・出題キュー飽和算術・理解トラッカーを実装。復習クイズをラジオ番組に音声注入する。
- **API ドキュメント**: Swagger(swaggo、`/swagger/` で配信、フロントエンドの型生成元)。
- **ロギング**: slog(JSON)。メトリクス基盤は持たない(フォールバック発生は `summaries.provider` で事後観測)。

---

## セットアップと起動

### 前提

- Docker / Docker Compose(Pi 上での server + worker + PostgreSQL 実行)
- radio バッチ用に Mac 側で: Go 1.25.6、[VOICEVOX Engine](https://voicevox.hiroshiba.jp/)、[Ollama](https://ollama.com/)、ffmpeg、rsync

### 開発環境

Makefile 経由で Docker 上の開発コンテナを操作します。

```bash
cp .env.example .env          # 環境変数を設定(下表参照)
make setup                    # 開発コンテナのビルド + 環境起動
make dev-up                   # コンテナ起動
make test                     # go test -race ./...(コンテナ内)
make lint                     # golangci-lint
make swagger                  # Swagger ドキュメント再生成
make admin-hash               # 管理者パスワードの bcrypt ハッシュ生成
make dev-down                 # 停止
```

主な Make ターゲット: `dev-up` / `dev-down` / `dev-shell` / `build` / `test` / `test-unit` / `test-coverage` / `lint` / `lint-fix` / `fmt` / `swagger` / `swagger-host` / `admin-hash` / `db-reset` / `db-shell` / `logs` / `clean`(一覧は `make help`)。

#### Swagger 生成物: clone 後に1度だけ生成が必要

`cmd/server` が swag 生成物(`docs/docs.go`。`.gitignore` 済みで追跡されない)をブランクインポートするため、未生成のまま `./...` をビルド/テストすると `package catchup-feed/docs is not in std` で失敗します。**clone 直後に1度、およびハンドラのアノテーションを変えたときに**生成してください。

```bash
make swagger        # Docker 経由(dev コンテナ内で生成)
make swagger-host   # Docker を使わない場合(= go tool swag init -g cmd/server/main.go --output docs --parseDependency --parseInternal)
```

`make test` / `make lint` / `make ci` は生成を含みません。dev コンテナは `compose.yml` の `- .:/app` でホストのツリーをそのままマウントするため、ホストの `docs/` に生成物が無ければコンテナ内でも同じエラーで失敗します。逆に `make swagger` の生成物はマウント経由でホストの `docs/` に残るので、一度実行すれば以後の `make test` / `make lint` とホストの `go build ./...` / `go test -race ./...` はそのまま通ります。CI は Go を使う5ジョブ(Test / Lint / Build / Security Scan / Dependency Vulnerability Scan)が各自 `swag init` を実行するため、この手順に依存しません。

`go tool swag` は go.mod の `tool` ディレクティブで固定した swag(v1.16.6。`--version` は upstream のバージョン定数が未更新のため `v1.16.4` と表示されますが、実体は `go list -m github.com/swaggo/swag` のとおり v1.16.6 です)を使います。CI の5ジョブと `Dockerfile` のビルドステージも同じ `go tool swag init` を使うため、**swag のバージョンは go.mod 一箇所で決まります**(`docs/docs.go` は `cmd/server` にブランクインポートされ本番バイナリに入るので、ここが浮動すると本番の再ビルドが上流の更新で落ちうる)。バージョンを上げるときは `go get github.com/swaggo/swag@vX.Y.Z` の1回で全経路に反映されます。なお `./cmd/radio` 単体のビルドは `docs` に依存しないため生成なしで通ります(後述の Mac の radio ビルド手順は影響を受けません)。

### server + worker(ローカル / Pi)

ルートの `compose.yml` が開発・本番共通のベース(`postgres` / `server` / `worker`、コンテナ名 `catchup-feed-*`)で、本番 Pi では `deploy/compose.pi.yml` を override として重ねます(`docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env up -d`。詳細は `deploy/pi.md`)。

```bash
cp .env.example .env          # POSTGRES_PASSWORD / JWT_SECRET / ADMIN_PASSWORD_HASH は必須
docker compose up -d --build
curl -f http://127.0.0.1:8090/health
```

server は起動時に PostgreSQL のマイグレーションを冪等適用してから待ち受けます(ホスト公開は `127.0.0.1:${API_PORT:-8090}` のみ。私的フィードリスナー 8081 は開発ではホストへ公開されません)。mp3 / 書籍 PDF はリポジトリ内 `./data/episodes` / `./data/books`(gitignore 済み)にマウントされます。

> **旧スタックからの移行メモ**: compose プロジェクト名を `catchup-feed` に変更したため、旧プロジェクト名 `catchup-feed-backend` で作られたコンテナ(`catchup-*`)・ボリューム(`catchup-feed-backend_db-data` 等)は `make down` / `make clean` の管理外に残ります。一度だけ `docker compose -p catchup-feed-backend down` で旧コンテナを掃除してください(**`-v` は付けない**。旧ボリュームの削除はデータ不要を確認したうえで `docker volume rm` で任意に)。

### radio(Mac、夜間バッチ)

radio は Mac ネイティブでビルドし、launchd で 04:30 JST に起動します。tailnet 越しに Pi の PostgreSQL へ直接接続します。

```bash
go build -o radio ./cmd/radio
./radio                       # 当日分エピソードを生成
./radio -dry-run              # 台本のみ生成して stdout へ出力(TTS / DB 書き込みなし)
./radio -since 2026-07-04T00:00:00+09:00   # 記事選定カーソルを手動指定して再実行
```

---

## 環境変数

`.env.example` にテンプレートがあります。以下はバイナリが実際に読む主要な変数です(既定値があるものは未設定でも動作します)。

### 共通 / データベース

| 変数 | 説明 |
|---|---|
| `DATABASE_URL` | PostgreSQL 接続文字列(必須) |
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | Compose の PostgreSQL 初期化 |
| `LOG_LEVEL` | `debug` で詳細ログ(既定は info) |
| `DB_MAX_OPEN_CONNS` / `DB_MAX_IDLE_CONNS` / `DB_CONN_MAX_LIFETIME` / `DB_CONN_MAX_IDLE_TIME` | コネクションプール調整 |

### server(管理 API・フィード配信)

| 変数 | 説明 |
|---|---|
| `JWT_SECRET` | 管理 API 用 JWT 署名鍵(32文字以上、必須) |
| `ADMIN_USER` / `ADMIN_PASSWORD_HASH` | 単一管理者の資格情報(パスワードは bcrypt ハッシュ、`make admin-hash` で生成) |
| `FEED_PUBLIC_BASE_URL` | 公開フィードの基底 URL(例: `https://radio.catchup-feed.com`) |
| `FEED_PRIVATE_BASE_URL` | 私的フィードの基底 URL(空なら Host ヘッダから導出) |
| `FEED_AUDIO_DIR` | mp3 アーカイブのディレクトリ(パストラバーサルガードの基準) |
| `BOOKS_DIR` | 書籍 PDF の格納ディレクトリ(D-25、既定 `books`)。`BOOKS_DIR/ファイル名` の正準絶対パスが書籍の同一性キー(books.file_path と book_ingest ジョブ payload に記録)。アップロード(100MB 上限)・一覧・削除は `/books`(JWT)、Mac worker への PDF 配信は `GET /private/books/{filename}`(tailnet 限定)。取り込みステータスは jobs から導出し、CLI 取り込み書籍(`deletable=false`)は API から削除不可 |
| `FEED_CHANNEL_TITLE` / `FEED_CHANNEL_DESCRIPTION` / `FEED_MAX_ITEMS` | RSS チャンネルメタデータ |
| `PRIVATE_FEED_ADDR` | tailnet 限定リスナーのバインドアドレス(例: `100.64.0.1:8081`。空で無効。ワイルドカードバインドは拒否) |
| `CORS_ALLOWED_ORIGINS` / `CORS_ALLOWED_METHODS` / `CORS_ALLOWED_HEADERS` / `CORS_MAX_AGE` | CORS 設定 |
| `CSP_ENABLED` / `CSP_REPORT_ONLY` | Content-Security-Policy |
| `RATE_LIMIT_TRUST_PROXY` / `RATE_LIMIT_TRUSTED_PROXIES` | レート制限の送信元 IP 判定。`true` のとき、列挙した CIDR からのリクエストに限り `X-Forwarded-For` を信頼する(Cloudflare Tunnel 配下の本番向け)。既定は無効 = `RemoteAddr` のみを見る |
| `AUTH_COOKIE_DOMAIN` | 認証 cookie の Domain 属性(D-22)。空なら属性を付けず応答ホストに限定(localhost 開発の既定)。本番は `.catchup-feed.com` |
| `PAGINATION_DEFAULT_PAGE` / `PAGINATION_DEFAULT_LIMIT` / `PAGINATION_MAX_LIMIT` | 一覧 API のページネーション(既定 1 / 20 / 100) |

> レート制限そのものは**常時有効**で、無効化する環境変数はありません(`/auth/token` 5 req/min、検索 100 req/min、公開フィード 60 req/min の固定値)。

### 要約 LLM(worker・radio 共通)

| 変数 | 説明 |
|---|---|
| `GEMINI_API_KEY` / `GEMINI_MODEL` | 第1段(無料枠)。キー未設定なら連鎖から除外。モデルは既定 `gemini-2.5-flash` |
| `GROQ_API_KEY` / `GROQ_MODEL` | 第2段(無料枠)。キー未設定なら連鎖から除外。モデルは既定 `openai/gpt-oss-120b`(D-41) |
| `OLLAMA_ENABLED` / `OLLAMA_HOST` / `OLLAMA_MODEL` | 最終段(ローカルフォールバック)。`OLLAMA_ENABLED` は未設定=有効で、無効化できるのは ParseBool が false と解釈する値(`false` / `FALSE` / `0` / `f` 等)のみ。解釈できない値は警告のうえ有効へ倒す(fail-open)。ホストは既定 `http://localhost:11434` — **コンテナ内では worker 自身を指す**ので、Docker Desktop(Mac)からホストの Ollama に届かせるには `http://host.docker.internal:11434`。モデルは既定 `qwen2.5:7b` |
| `SUMMARIZER_TIMEOUT` / `SUMMARIZER_CHAR_LIMIT` | 要約タイムアウト(既定 `60s`。不正値は警告して既定へ戻す)・要約の文字数上限(既定 `900`、許容 100〜5000。範囲外・不正値は警告して既定へ戻す) |

> **記事本文の切り詰め基準は環境変数ではありません**(出力側の上限は上表の `SUMMARIZER_CHAR_LIMIT`)。
> 記事本文はプロンプト化の前に `maxInputChars = 10000`(`internal/infra/summarizer/provider.go`)で
> 切り詰められます。これは**文字数ではなく byte 数**で、切断位置は UTF-8 のルーン境界まで戻されます
> (日本語本文なら概ね 3,300 文字強)。この 10,000 byte は本文に対する基準にすぎず、切り詰め通知と
> プロンプト前置文が足されるぶん、**プロバイダに渡るプロンプト全体は 10,000 byte を超えます**。
> 長すぎる記事の要約が尻切れに見えるときはここを疑う。台本生成に使う `Generate` は切り詰めを
> 行いません(プロンプト長は呼び出し側の責任 — D-3)。

### worker(クロール・ジョブ)

| 変数 | 説明 |
|---|---|
| `CONTENT_FETCH_ENABLED` / `CONTENT_FETCH_THRESHOLD` / `CONTENT_FETCH_PARALLELISM` / `CONTENT_FETCH_TIMEOUT` | go-readability 本文抽出 |
| `CONTENT_FETCH_MAX_REDIRECTS` / `CONTENT_FETCH_DENY_PRIVATE_IPS` / `CONTENT_FETCH_MAX_BODY_SIZE` | SSRF ガード・取得上限 |
| `NEWSLETTER_MAX_ARTICLES` | newsletter ソース(リンク集型)の号あたり展開記事数上限(既定 10)。号内リンクを文書順の先頭から N 件だけ fetch → 要約する |
| `JOBS_POLL_INTERVAL` | jobs コンシューマのポーリング間隔(既定 10s) |
| `CRON_SCHEDULE` | クロールのスケジュール(compose の既定 `0 * * * *` = 毎時。Go 側の既定値は `30 5 * * *`)。前回実行が次の発火に届いていたら重ねずスキップする |
| `CRAWL_TIMEOUT` | 1 サイクルの上限時間(既定 30m、許容 1m〜4h)。範囲外は警告して既定へ戻す |
| `WORKER_TIMEZONE` | cron のタイムゾーン(既定 `Asia/Tokyo`) |
| `WORKER_HEALTH_PORT` | worker のヘルスチェックポート(既定 9091、許容 1024〜65535) |
| `CLEANUP_CRON_SCHEDULE` | mp3 保持ジョブの投入スケジュール(既定 `30 6 * * *`) |

### radio(音声生成・TTS)

| 変数 | 説明 |
|---|---|
| `RADIO_SHOW_NAME` | 番組名(エピソードタイトルと ID3 タグ用。**台本の読み上げ名は連動せず** `internal/script/format.go` に固定 — D-37) |
| `RADIO_MAX_ARTICLES` | 1エピソードの最大記事数(既定 8) |
| `RADIO_EPISODES_DIR` | Mac 側の一時生成ディレクトリ(既定 `/data/episodes`) |
| `RADIO_RSYNC_DEST` / `RADIO_RSYNC_PATH` | Pi への rsync 転送先(空ならローカル配置) |
| `RADIO_TIMEZONE` | 放送日判定のタイムゾーン(既定 `Asia/Tokyo`) |
| `RADIO_TIMEOUT` | ラン全体のタイムアウト(既定 1h) |
| `VOICEVOX_URL` | VOICEVOX Engine のエンドポイント(既定 `http://127.0.0.1:50021`) |
| `VOICEVOX_SPEAKER` / `VOICEVOX_SPEAKER_NAME` | 話者 style ID(コード既定 3 = ずんだもん。**実運用は 30 = No.7 アナウンス** — D-2)/ クレジット表記用の話者名(未設定なら Engine の `/speakers` から解決。両方失敗なら当日スキップ — U-13) |
| `VOICEVOX_SPEED_SCALE` / `VOICEVOX_TIMEOUT` | 話速 / 合成タイムアウト |
| `FFMPEG_PATH` | ffmpeg のパス |
| `RADIO_LEARNING_URL` | 私的版ショーノートに載せる採点ページ URL(既定 `https://pulse.catchup-feed.com/learning`)。聴取 → 採点の唯一の導線 |
| `BOOK_REVIEW_OLLAMA_MODEL` / `BOOK_REVIEW_CHUNKS` | 書籍コーナー(私的データ)のローカルモデル(既定 `gemma4:12b`)・1 回あたりのチャンク数(既定 3) |
| `PRIVATE_EPISODE_MAX_MINUTES` | 私的版の尺の上限(既定 18 分)。ニュース + 復習 + 振り返り + ジングルがこれに迫る日は書籍コーナーを翌日へ回す(カーソルは進めない) |

> **ソースのカテゴリを増やしたら `internal/script/format.go` の `cornerNameBySlug` に1行足すこと。**
> `sources.category` はダッシュボードから自由入力でき、DB の CHECK 制約も無い。対応表に無いスラッグは
> 読み上げコーナー名としてそのまま使われる(= 英字がそのまま音声になる)。当日の radio ログに
> `unknown source category` の WARN が出るので、それを見て追記する。

### 学習ループ(Phase 3)

| 変数 | 説明 |
|---|---|
| `QUIZ_LADDER_DAYS` | spaced repetition の間隔ラダー |
| `QUIZ_ITEMS_PER_DAY` / `QUIZ_SLOTS` | 1日の生成項目数・出題スロット数 |
| `QUIZ_AUTO_RESOLVE_AFTER` / `QUIZ_BACKPRESSURE_THRESHOLD` / `QUIZ_WEEKLY_REVIEW_DOW` | 自動採点・キュー飽和・週次振り返り曜日 |

### 通知

| 変数 | 説明 |
|---|---|
| `SMTP_ENABLED` | メール通知(SMTP)の有効化。本人向け通知(D-29)専用。友人向けエピソードメールは D-32 で廃止(周知はポッドキャストアプリの購読のみ) |
| `NOTIFY_ERROR_EMAIL_TO` | 本人向け通知(障害・新着エピソード)の宛先アドレス。空なら本人向け通知は無効 |

SMTP 認証情報などの機密値は `.env.example` のコメントを参照してください。秘密情報はコードやリポジトリにコミットしないでください。

---

## テスト

```bash
make test          # go test -race ./...(コンテナ内)
make test-unit     # 短縮ユニットテスト
make test-coverage # カバレッジ HTML 生成
```

テストは table-driven + testify。フィードのトークン検証(失効・不正トークン)と Range 配信(境界)には専用のテストがあります。

---

## ドキュメント

リポジトリ内:

- [docs/architecture.md](docs/architecture.md) — 層構成・依存ルール・データフロー・縮退設計・技術選定
- [docs/repository-structure.md](docs/repository-structure.md) — ディレクトリとパッケージの責務、配置規約
- [deploy/README.md](deploy/README.md) — Pi / Mac の導入・運用手順の入口

Phase 別の設計は親リポジトリの `docs/` にあります(このリポジトリと食い違う場合は設計書が正)。

- Phase 1 — ラジオ配信基盤(構成・データモデル・フィード/トークン認証・通知)
- Phase 2 — ソース多モーダル化(YouTube/ポッドキャスト取り込み)+ 書籍 PDF RAG
- Phase 3 — 学習ループコア(理解トラッカー・spaced repetition・復習クイズの音声注入)

API 仕様は server 起動後に `/swagger/` で確認できます。

---

## ライセンス

[MIT License](LICENSE)
