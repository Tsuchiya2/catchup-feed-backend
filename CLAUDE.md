# catchup-feed-backend(pulse バックエンド)

Go 1.27.0 単一モジュール(module name: `catchup-feed`)。バージョンは `.go-version` / `go.mod` / `Dockerfile` / CI の `GO_VERSION` の**機械可読4箇所**で揃え、散文6箇所(README 3・`docs/architecture.md`・`docs/repository-structure.md`・この CLAUDE.md)も追随させる。**さらに M3 Mac の実 Go ツールチェーンも上げる** — radio はそのマシンの Go でビルドされ、CI では取りこぼしを検出できない(D-45、手順は `deploy/mac.md` §5)。Phase 1〜3 のコード実装は全件完了し、Pi + Mac で運用中。全体像と規約は親ディレクトリの `CLAUDE.md`、Phase 別設計は `docs/pulse-phase1〜3-design.md` と `docs/decisions.md` が正。ここにはリポジトリ固有の事項のみ書く。

## バイナリ構成

- `cmd/server` — Pi 常駐。公開: フィード配信(`/feeds/{token}/*`、URL 埋め込みトークン認証)+ 管理 API(JWT、C-21 フラット構成: `/sources` `/articles` `/subscribers` `/tokens` `/access-logs` `/learning/*` `/books` `/viewers` `/auth/*`)。tailnet 別リスナー: 私的フィード `/private/*` + Mac worker 向け PDF 配信 `GET /private/books/{file}`(D-25)。起動時に冪等マイグレーション(`internal/infra/db.MigrateUp`)を自動適用 — 専用バイナリはない
- `cmd/worker` — Pi 常駐。robfig/cron で毎時クロール → 本文抽出 → 要約 → 未要約記事の掃き取り(§5.2b)。`jobs` テーブルのコンシューマとして `regenerate_feed` / `notify_episode` / `notify_error` / `cleanup_old_media` を処理(`transcribe` / `book_ingest` は Mac の catchup-feed-ai が claim する — 各コンシューマは自 kind のみスイープする契約)
- `cmd/radio` — Mac 夜間バッチ(launchd 04:30)。記事選定 → 台本生成 → VOICEVOX → ジングル結合(D-36)→ mp3 → rsync → `episodes`/`segments` 登録。同一ランで学習項目生成・クイズ/週次振り返り/書籍コーナー入りの私的版も作る(Phase 3)
- 補助: `cmd/hash-password`(`make admin-hash`)、`cmd/crawl-once`(開発用の単発クロール。現状維持で確定)

## このリポジトリの約束

- ルーターは net/http 標準(外部ルーターなし)、DB は pgx/v5、テストは table-driven + testify。`go test -race ./...` と `go vet ./...` が完了条件。Makefile のターゲットを使う(`make test` / `lint` / `swagger` / `admin-hash` 等)
- API 契約は swag の Swagger 生成が正(C-19)。ハンドラのアノテーションを変えたら `make swagger` を回し、frontend 側の `npm run generate:api` 用に swagger.json を引き渡す
- clone 後は `./...` をビルド/テストする前に Swagger を1度生成する(Docker 経由なら `make swagger`、Docker なしなら `make swagger-host` = `go tool swag init -g cmd/server/main.go --output docs --parseDependency --parseInternal`)— `cmd/server` が swag 生成物の `docs` をブランクインポートし、`docs/docs.go` は gitignore 済みなので未生成だと `package catchup-feed/docs is not in std` で落ちる。`make test` / `lint` / `ci` は生成を含まず、dev コンテナは `- .:/app` でホストのツリーをマウントするためコンテナ内でも同じエラーになる(生成物はマウント経由でホストの `docs/` に残るので1度で済む)。CI は Test / Lint / Build / Security Scan / Dependency Vulnerability Scan の各ジョブが自前で生成する。`./cmd/radio` 単体は `docs` 非依存で生成なしでも通る
- ハンドラのルート登録は各パッケージの `register.go` に `Register(mux, service, ...)` を実装し、`cmd/server` はそれを呼ぶだけ。設計判断はコメントに決定ログ番号(D-xx / C-xx / §x.x)を残す
- `internal/learning/` は Phase 3 学習コア: SRS 遷移純関数(§6.1)・JST 放送日ヘルパ(§12-10)・クイズパラメータ config(D-18)。radio と server が共有するため、radio/server/handler/repository へ依存させない
- 番組の言い回し(番組名の読み・コーナー名・定型句・固定文)は `internal/script/format.go` にのみ置く(D-37)。`prompts/*.tmpl` と `internal/script/*.go` への文言の直書きは `TestSpokenWordingLivesOnlyInFormatGo` が落とす
- `internal/tts/assets/`(ジングル)と `internal/feed/assets/`(アートワーク)は go:embed。差し替えは「ファイル置換 + 再ビルド」で、Mac の radio は `git pull` だけでは反映されない
- 新しい env を足したら `.env.example`・`deploy/env.pi.example` / `env.mac.example` と `compose.yml` / `deploy/compose.pi.yml` の environment(明示 allowlist)も同時に更新する — 配線漏れで本番ログイン不能を起こした前例あり
- ディレクトリとパッケージの責務は [docs/repository-structure.md](docs/repository-structure.md)、層の設計思想は [docs/architecture.md](docs/architecture.md)。初代 EDAF 期の docs(development-guidelines / functional-design / product-requirements / glossary)は 2026-08-13 に削除済み — 復元が必要なら git 履歴を参照
- **dependabot の gomod PR は `github.com/swaggo/swag` を `// indirect` に降格させることがある**(#122 で実際に起きた)。swag は生成物 `docs/docs.go` が import する直接依存だが、dependabot は gitignore された生成物の無いツリーで tidy するため誤判定する。マージ時は direct のままか確認する — 放置すると Swagger 生成済みの正規状態で `go mod tidy` を回した全員に差分が出続ける
- コミットメッセージ・PR に Co-Authored-By 行を付けない

## 環境変数

全量は `.env.example`(開発)/ `deploy/env.pi.example`(Pi)/ `deploy/env.mac.example`(Mac)と README の一覧が正。要点のみ:

- server: `DATABASE_URL` / `JWT_SECRET` / `ADMIN_USER`・`ADMIN_PASSWORD_HASH`(bcrypt)/ `AUTH_COOKIE_DOMAIN`(D-22 の HttpOnly cookie)/ `FEED_PUBLIC_BASE_URL`(D-6)/ `PRIVATE_FEED_ADDR`(tailnet 限定、ワイルドカードバインドは起動拒否)/ `BOOKS_DIR`(D-25)
- 要約連鎖: `GEMINI_*` → `GROQ_*` → `OLLAMA_*`(worker / radio 共通)
- radio: `VOICEVOX_*`(実運用の話者は `VOICEVOX_SPEAKER=30` = No.7、D-2)/ `RADIO_*` / `BOOK_REVIEW_*` / `PRIVATE_EPISODE_MAX_MINUTES`
- 学習: `QUIZ_*`(ラダー・出題枠・自動解決・バックプレッシャ・週次曜日、D-18/D-21)
- 通知: `SMTP_*` + `NOTIFY_ERROR_EMAIL_TO`(本人=管理者宛のみ。Discord/Slack は D-29 で、友人向けメールは D-32 で廃止)

秘密情報はコードとリポジトリに置かない。
