# catchup-feed-backend(pulse バックエンド)

Go 1.25 単一モジュール。pulse Phase 1 の中核。全体像と規約は親ディレクトリの `CLAUDE.md` と `docs/pulse-phase1-design.md` が正。ここにはリポジトリ固有の事項のみ書く。

## バイナリ構成(目標)
- `cmd/server` — Pi 常駐。公開: フィード配信(/feeds/{token}/*)+管理 API(JWT)。tailnet: 私的フィード(/private/*)
- `cmd/worker` — Pi 常駐。robfig/cron でクロール → 要約 → 通知。jobs テーブルのコンシューマ
- `cmd/radio` — Mac 夜間バッチ。台本構成 → VOICEVOX(HTTP API 直叩き)→ ffmpeg 結合 → rsync → episodes 登録
- `cmd/migrate` — 既存方式を継続
- `cmd/ai`, `cmd/api`, `cmd/crawl-once` — 旧構成。減量調査の結果に従い整理

## このリポジトリの約束
- ルーターは net/http 標準、DB は pgx/v5、テストは table-driven + testify。`go test -race ./internal/...` と `go vet ./...` が完了条件
- Makefile のターゲットを使う(db-migrate 等)。新ターゲット追加時は既存の書式に合わせる
- 旧コードの削除対象・流用対象は `.claude/agents/go-backend.md`(親側)の一覧が正。判断に迷う境界のコードは親に確認
- `internal/learning/` は Phase 3 予約。ディレクトリだけ作りコードを置かない

## 環境変数(実装時に README へ反映)
DB 接続、JWT シークレット、管理者資格情報(bcrypt ハッシュ)、Gemini/Groq API キー、Ollama エンドポイント、VOICEVOX エンドポイント、Discord/Slack Webhook、SMTP、フィード公開ドメイン(D-6 決定後)。秘密情報はコードとリポジトリに置かない。
