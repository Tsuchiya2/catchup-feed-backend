# deploy/ — pulse デプロイ・運用資材

Phase 1 設計書 §3(ホスト配置)・§8(バックアップ・縮退)・§9(旧システム停止)と
Phase 2 設計書 §3・§6・§7(transcribe worker / Open WebUI / pgvector)の実体。
入口はこの4つ:

| ファイル | 対象 | 内容 |
|---|---|---|
| [pi.md](pi.md) | Raspberry Pi 5 | PostgreSQL / server / worker / episodes / Cloudflare Tunnel |
| [mac.md](mac.md) | M3 MacBook Pro | VOICEVOX / Ollama / ffmpeg / radio / launchd / バックアップ(11章〜: Phase 2 の Open WebUI・モデル追加) |
| [ai.md](ai.md) | M3 MacBook Pro | Phase 2: transcribe worker(catchup-feed-ai)導入と他サービス連携(D-10) |
| [legacy-shutdown.md](legacy-shutdown.md) | Pi | 旧 catchup-feed の計画停止(§9) |

構成物:

```
deploy/
├── compose.pi.yml            # Pi 用 compose(pulse スタック。旧スタックと共存可)
├── env.pi.example            # Pi 側 .env の雛形(キーのみ。値は書かない)
├── env.mac.example           # Mac 側 ~/pulse/.env の雛形(同上)
├── cloudflared/
│   └── config.example.yml    # 既存トンネルへの ingress 追加例
├── systemd/
│   └── pulse.service         # ブート時に tailscaled 後で compose up する oneshot
├── launchd/
│   ├── com.pulse.transcribe.plist  # 03:00 JST 文字起こし(Phase 2、deadline 04:15)
│   ├── com.pulse.radio.plist       # 04:30 JST radio バッチ
│   └── com.pulse.backup.plist      # 05:15 JST DB バックアップ(Mac が Pi から pull)
└── scripts/
    ├── transcribe-run.sh     # launchd → pulse-transcribe のラッパー(.env 読込 + 04:30 へのブリッジ)
    ├── radio-run.sh          # launchd → radio のラッパー(.env 読込 + VOICEVOX 起動)
    └── backup-pulse-db.sh    # pg_dump pull + episodes ミラー(Mac 側で実行)
```

原則(CLAUDE.md 準拠): ゼロ円 / 監視スタックなし / 縮退許容(リトライ装置を付けない)。
秘密の値(パスワード・API キー・Webhook URL)はこのディレクトリにもチャットにも書かない。
`.env` に直接記入する(`.gitignore` 済み)。
