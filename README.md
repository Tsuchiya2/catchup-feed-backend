# pulse — backend

> 毎朝10〜15分の音声ラジオ番組をポッドキャストアプリに配信し、「理解の定着」を最適化する個人向け学習システムのバックエンド。

`pulse` は旧 catchup-feed(RSS を要約して REST API / Discord に流す news aggregator)の後継です。旧システムは「配信された記事数」を最適化していましたが、Discord に流れる要約は読まれませんでした。pulse が最適化するのは **理解の定着** です。可処分時間が細切れで手も目も塞がっている時間帯(移動中・家事中)に消化できるよう、応答形態を **音声** に変えました。RSS を要約し、毎朝ラジオ番組(mp3)を生成し、ポッドキャストアプリ経由で本人と友人に届け、フィードバックを得ます。

このリポジトリは pulse の Go バックエンドです。フロントエンド(ダッシュボード)は [catchup-feed-frontend](https://github.com/Tsuchiya2/catchup-feed-frontend)、文字起こし・書籍 RAG は [catchup-feed-ai](https://github.com/Tsuchiya2/catchup-feed-ai) にあります。

---

## 設計原則

pulse は**単一ユーザー**が**ゼロ円**で運用する自宅ホスティングを前提に「右サイズ」で作られています。旧 catchup-feed が抱えていた gRPC・サーキットブレーカー・Prometheus・Grafana・OpenTelemetry・マイクロサービス分割・OpenAI/Claude 依存はすべて**削除済み**です。

- **単一ユーザー右サイズ** — 冗長化・可観測性基盤・内部 RPC を持たない。プロセス間連携は PostgreSQL のジョブテーブルのみ。
- **ゼロ円運用** — 要約は無料枠 API → ローカル LLM のフォールバック連鎖。有料 API・有料 SaaS を使わない。
- **縮退許容** — 「壊れない」より「壊れても翌日勝手に戻る」。Mac 不在 → その日のエピソードは欠番、無料 API 全滅 → Ollama にフォールバック、VOICEVOX 障害 → 当日スキップ+通知。
- **プライバシー分界** — 無料クラウド API に流してよいのは公開記事とその要約のみ。書籍・私的データはローカル LLM(Ollama)のみで処理する。

---

## アーキテクチャ

Go 1.26 の単一モジュールで、**3つのバイナリ**を持ちます。内部 HTTP/RPC はなく、`server` / `worker` / `radio` はすべて **PostgreSQL 経由**(`jobs` テーブル+状態テーブル)で連携します。

| バイナリ | 配置 | 役割 |
|---|---|---|
| `cmd/server` | Pi 5(常駐) | 公開フィード配信(`/feeds/{token}/*`、トークン認証)+ 管理 API(JWT)+ tailnet 限定の私的フィード(`/private/*`)。起動時に冪等マイグレーションを自動適用。 |
| `cmd/worker` | Pi 5(常駐) | robfig/cron で毎時クロール → 本文抽出 → 要約 → DB 更新。`jobs` テーブルのコンシューマとして `regenerate_feed` / `notify_episode` / `notify_error` / `cleanup_old_media` を処理。 |
| `cmd/radio` | M3 Mac(夜間バッチ) | 記事選定 → LLM 台本生成 → VOICEVOX で音声合成 → ffmpeg で結合・mp3 化 → rsync で Pi へ転送 → `episodes`/`segments` を登録。Phase 3 のクイズ・書籍コーナーも同一ランで生成。 |

補助バイナリ: `cmd/hash-password`(管理者パスワードの bcrypt ハッシュ生成)、`cmd/crawl-once`(開発用の単発クロール)。

### ホスト配置

```
┌──────────── Raspberry Pi 5(常時稼働)──────────────┐
│  server  : 公開フィード配信 / 管理 API / 私的フィード  │
│  worker  : クロール・要約・通知(cron 常駐)          │
│  PostgreSQL + mp3 アーカイブ(episodes/)             │
└──────────────────────────────────────────────────┘
          ▲ Tailscale(rsync / PostgreSQL 接続)
┌──────────── M3 MacBook Pro(夜間バッチ)────────────┐
│  radio   : 台本構成 → VOICEVOX → ffmpeg 結合         │
│  VOICEVOX Engine(ずんだもん)                        │
│  Ollama(要約・書籍のローカル LLM フォールバック)     │
└──────────────────────────────────────────────────┘

公開経路: Cloudflare Tunnel → Pi server
  - pulse.catchup-feed.com         → Next.js ダッシュボード(JWT 保護)
  - radio.catchup-feed.com/feeds/* → 公開フィード(トークン認証)
私的経路: Tailscale(tailnet 内のみ、認証は物理境界)
  - pi.tailnet:8081/private/feed.xml
```

### 日次フロー

```
[worker/Pi]  毎時       : クロール → articles 挿入 → 要約 → summaries 更新
[radio/Mac]  04:30 JST  : 当日分エピソード生成
   1. 対象記事選定(前回エピソード以降の要約済み記事)
   2. 台本生成(LLM): セグメントごとの読み上げ原稿+つなぎ文
   3. VOICEVOX でセグメント別に合成 → ffmpeg で結合・mp3 化(64kbps mono)
   4. rsync で Pi の episodes/ へ転送、episodes / segments を INSERT
      → jobs に regenerate_feed / notify_episode を積む
[worker/Pi]  ジョブ検知  : フィード XML 再生成 → Discord/Slack/メール通知
```

Mac が閉じていた日はエピソードが生成されないだけで、システムは壊れません(翌日に持ち越し)。

---

## 技術スタック

- **言語 / ランタイム**: Go 1.26.x(単一モジュール、標準ライブラリの `net/http` ルーター — 外部ルーター依存なし)
- **データベース**: PostgreSQL(ドライバは pgx/v5)。マイグレーションは `cmd/server` 起動時に冪等 SQL を自動適用。
- **認証**: 管理 API は JWT(golang-jwt/v5)+ 単一管理者(環境変数 + bcrypt ハッシュ)。フィード配信は URL 埋め込みの不透明トークン(`crypto/rand` 32byte → base64url、DB には SHA-256 ハッシュのみ保存)。
- **クローラー**: gofeed(RSS/Atom パース)+ go-readability(本文抽出)。リダイレクトごとに SSRF ガード。
- **要約 LLM(フォールバック連鎖)**: Gemini → Groq → Ollama。無料枠 API が全滅してもローカル(Ollama)で縮退継続。API キー未設定のプロバイダは連鎖から自動除外。
- **音声合成 (TTS)**: VOICEVOX(HTTP API を直叩き、既定話者はずんだもん)。
- **音声処理**: ffmpeg(結合・loudnorm・mp3 エンコード)、rsync(Pi への転送)を `exec.Command` で呼び出し。
- **スケジューラ**: robfig/cron(worker)、launchd(radio の夜間起動)。
- **学習ループ(Phase 3)**: `internal/learning/` に spaced repetition(SRS)の間隔ラダー・出題キュー飽和算術・理解トラッカーを実装。復習クイズをラジオ番組に音声注入する。
- **API ドキュメント**: Swagger(swaggo、`/swagger/` で配信、フロントエンドの型生成元)。
- **ロギング**: slog(JSON)。メトリクス基盤は持たない(フォールバック発生は `summaries.provider` で事後観測)。

---

## セットアップと起動

### 前提

- Docker / Docker Compose(Pi 上での server + worker + PostgreSQL 実行)
- radio バッチ用に Mac 側で: Go 1.26.x、[VOICEVOX Engine](https://voicevox.hiroshiba.jp/)、[Ollama](https://ollama.com/)、ffmpeg、rsync

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

主な Make ターゲット: `dev-up` / `dev-down` / `dev-shell` / `build` / `test` / `test-unit` / `test-coverage` / `lint` / `lint-fix` / `fmt` / `swagger` / `admin-hash` / `db-reset` / `db-shell` / `logs` / `clean`(一覧は `make help`)。

### server + worker(Pi)

Docker Compose で `postgres` / `app`(server) / `worker` を起動します。

```bash
docker compose up -d
```

server は起動時に PostgreSQL のマイグレーションを冪等適用してから `:8080` で待ち受けます(`PRIVATE_FEED_ADDR` を設定すると tailnet 用の私的フィードリスナーを別ポートで起動)。

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
| `FEED_CHANNEL_TITLE` / `FEED_CHANNEL_DESCRIPTION` / `FEED_MAX_ITEMS` | RSS チャンネルメタデータ |
| `PRIVATE_FEED_ADDR` | tailnet 限定リスナーのバインドアドレス(例: `100.64.0.1:8081`。空で無効。ワイルドカードバインドは拒否) |
| `CORS_ALLOWED_ORIGINS` / `CORS_ALLOWED_METHODS` / `CORS_ALLOWED_HEADERS` / `CORS_MAX_AGE` | CORS 設定 |
| `CSP_ENABLED` / `CSP_REPORT_ONLY` | Content-Security-Policy |
| `RATELIMIT_ENABLED` / `RATE_LIMIT_TRUST_PROXY` / `RATE_LIMIT_TRUSTED_PROXIES` | レート制限(公開ルートは per-IP) |

### 要約 LLM(worker・radio 共通)

| 変数 | 説明 |
|---|---|
| `GEMINI_API_KEY` / `GEMINI_MODEL` | 第1段(無料枠)。キー未設定なら連鎖から除外 |
| `GROQ_API_KEY` / `GROQ_MODEL` | 第2段(無料枠)。キー未設定なら連鎖から除外 |
| `OLLAMA_ENABLED` / `OLLAMA_HOST` / `OLLAMA_MODEL` | 最終段(ローカルフォールバック) |
| `SUMMARIZER_TIMEOUT` / `SUMMARIZER_CHAR_LIMIT` | 要約タイムアウト・入力文字数上限 |

### worker(クロール・ジョブ)

| 変数 | 説明 |
|---|---|
| `CONTENT_FETCH_ENABLED` / `CONTENT_FETCH_THRESHOLD` / `CONTENT_FETCH_PARALLELISM` / `CONTENT_FETCH_TIMEOUT` | go-readability 本文抽出 |
| `CONTENT_FETCH_MAX_REDIRECTS` / `CONTENT_FETCH_DENY_PRIVATE_IPS` / `CONTENT_FETCH_MAX_BODY_SIZE` | SSRF ガード・取得上限 |
| `JOBS_POLL_INTERVAL` | jobs コンシューマのポーリング間隔 |
| `CLEANUP_CRON_SCHEDULE` | mp3 保持ジョブの投入スケジュール(既定 `30 6 * * *`) |

### radio(音声生成・TTS)

| 変数 | 説明 |
|---|---|
| `RADIO_SHOW_NAME` | 番組名 |
| `RADIO_MAX_ARTICLES` | 1エピソードの最大記事数(既定 8) |
| `RADIO_EPISODES_DIR` | Mac 側の一時生成ディレクトリ(既定 `/data/episodes`) |
| `RADIO_RSYNC_DEST` / `RADIO_RSYNC_PATH` | Pi への rsync 転送先(空ならローカル配置) |
| `RADIO_TIMEZONE` | 放送日判定のタイムゾーン(既定 `Asia/Tokyo`) |
| `RADIO_TIMEOUT` | ラン全体のタイムアウト(既定 1h) |
| `VOICEVOX_URL` | VOICEVOX Engine のエンドポイント(既定 `http://127.0.0.1:50021`) |
| `VOICEVOX_SPEAKER` / `VOICEVOX_SPEAKER_NAME` | 話者 ID(既定 3 = ずんだもん)/ クレジット表記用の話者名 |
| `VOICEVOX_SPEED_SCALE` / `VOICEVOX_TIMEOUT` | 話速 / 合成タイムアウト |
| `FFMPEG_PATH` | ffmpeg のパス |
| `BOOK_REVIEW_OLLAMA_MODEL` / `BOOK_REVIEW_CHUNKS` | 書籍コーナー(私的データ)のローカルモデル・チャンク数 |

### 学習ループ(Phase 3)

| 変数 | 説明 |
|---|---|
| `QUIZ_LADDER_DAYS` | spaced repetition の間隔ラダー |
| `QUIZ_ITEMS_PER_DAY` / `QUIZ_SLOTS` | 1日の生成項目数・出題スロット数 |
| `QUIZ_AUTO_RESOLVE_AFTER` / `QUIZ_BACKPRESSURE_THRESHOLD` / `QUIZ_WEEKLY_REVIEW_DOW` | 自動採点・キュー飽和・週次振り返り曜日 |

### 通知

| 変数 | 説明 |
|---|---|
| `DISCORD_ENABLED` | Discord Webhook 通知の有効化 |
| `SLACK_ENABLED` | Slack Webhook 通知の有効化 |
| `SMTP_ENABLED` | 友人へのメール通知(SMTP)の有効化 |

Webhook URL・SMTP 認証情報などの機密値は `.env.example` のコメントを参照してください。秘密情報はコードやリポジトリにコミットしないでください。

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

Phase 別の設計は親リポジトリの `docs/` にあります(このリポジトリと食い違う場合は設計書が正)。

- Phase 1 — ラジオ配信基盤(構成・データモデル・フィード/トークン認証・通知)
- Phase 2 — ソース多モーダル化(YouTube/ポッドキャスト取り込み)+ 書籍 PDF RAG
- Phase 3 — 学習ループコア(理解トラッカー・spaced repetition・復習クイズの音声注入)

API 仕様は server 起動後に `/swagger/` で確認できます。

---

## ライセンス

[MIT License](LICENSE)
