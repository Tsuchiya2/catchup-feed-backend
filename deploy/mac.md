# M3 Mac セットアップ手順(pulse Phase 1)

対象: M3 MacBook Pro(夜間バッチ)。radio + VOICEVOX Engine + Ollama を載せる(設計書 §3)。
Mac が閉じている・電源が無い日はエピソード欠番で**正常**(縮退許容)。ここに冗長化・リトライ装置は作らない。

前提: Homebrew、Tailscale 参加済み、Pi 側のセットアップ(deploy/pi.md 1〜4章)完了。

ディレクトリ規約(以下この配置を前提にする):

```
~/pulse/
├── bin/        radio バイナリ、radio-run.sh、backup-pulse-db.sh
├── logs/       launchd の出力・VOICEVOX ログ
├── backups/    db/(pg_dump)と episodes/(ミラー)
├── voicevox_engine/   VOICEVOX Engine 展開先
└── .env        環境変数(deploy/env.mac.example から作成)
```

```bash
mkdir -p ~/pulse/{bin,logs,backups}
```

---

## 1. ffmpeg(U-6)

```bash
brew install ffmpeg
ffmpeg -version
```

## 2. Ollama + 軽量モデル(U-5)

```bash
brew install ollama
brew services start ollama        # ログイン時に自動起動
ollama pull qwen2.5:7b            # コードの既定モデル(OLLAMA_MODEL で差し替え可)
curl -s http://127.0.0.1:11434/api/tags   # 応答があれば OK
```

## 3. Ollama を Pi の worker から使えるようにする(tailnet 限定公開)

Pi の worker は要約フォールバックの最終段として Mac の Ollama を叩く(§8)。Ollama 自体は localhost のまま、**Tailscale の TCP フォワードで tailnet にだけ**開ける(LAN には出さない):

```bash
tailscale serve --bg --tcp 11434 tcp://localhost:11434
tailscale serve status    # 設定は再起動後も保持される
```

Pi 側 `deploy/.env` の `OLLAMA_HOST` は **Mac の Tailscale IP**(Mac 上で `tailscale ip -4` で確認)を指す: `http://<Mac の Tailscale IP>:11434`。**MagicDNS 名(`.ts.net`)は使えない** — tailscale serve の TCP フォワードは Host ヘッダを素通しし、Ollama の Host ヘッダ検証が `.ts.net` 名を 403 で拒否するため(Tailscale IP は許可される。実測確認済み)。Mac がスリープ中は失敗するが、未要約記事は次回クロールに持ち越されるだけ(縮退)。

## 4. VOICEVOX Engine(GUI 不要のエンジン単体、U-6)

現行(2026-07 時点)の Apple Silicon 向け配布は GitHub Releases の `voicevox_engine-macos-arm64-<ver>.7z.001`(分割 7z)。Docker イメージは x86_64 のみなので Mac では**ネイティブバイナリ**を使う。最新版は https://github.com/VOICEVOX/voicevox_engine/releases で確認(以下は 0.25.2 の例。バージョン部分だけ読み替える):

```bash
brew install sevenzip
cd ~/pulse
curl -LO https://github.com/VOICEVOX/voicevox_engine/releases/download/0.25.2/voicevox_engine-macos-arm64-0.25.2.7z.001
7zz x voicevox_engine-macos-arm64-0.25.2.7z.001
mv macos-arm64 voicevox_engine          # 展開先ディレクトリ名は ls で確認して合わせる
rm voicevox_engine-macos-arm64-0.25.2.7z.001
xattr -dr com.apple.quarantine ~/pulse/voicevox_engine   # Gatekeeper の隔離属性を外す
chmod +x ~/pulse/voicevox_engine/run
```

起動確認(初回はモデル読み込みで1分程度かかる):

```bash
~/pulse/voicevox_engine/run --host 127.0.0.1 --port 50021 &
curl -s http://127.0.0.1:50021/version    # バージョン文字列が返れば OK
kill %1
```

常駐はさせない。夜間は `radio-run.sh` が起動→終了後に停止する(手動検証時も同様に自動起動される)。

## 5. radio バイナリのビルド

radio は Mac ネイティブで動かす(Docker 不要)。

```bash
brew install go
cd <このリポジトリの checkout>
go build -o ~/pulse/bin/radio ./cmd/radio
```

リポジトリ更新時は同じコマンドでビルドし直すだけ。

## 6. .env とラッパースクリプトの配置

```bash
cd <このリポジトリの checkout>
cp deploy/env.mac.example ~/pulse/.env
chmod 600 ~/pulse/.env
cp deploy/scripts/radio-run.sh deploy/scripts/backup-pulse-db.sh ~/pulse/bin/
chmod +x ~/pulse/bin/radio-run.sh ~/pulse/bin/backup-pulse-db.sh
```

`~/pulse/.env` を編集(**値はファイルに直接記入。チャット等に貼らない**)。特に注意する3キー:

| キー | 意味 |
|---|---|
| `DATABASE_URL` | Pi の PostgreSQL を **tailnet 越し**に指す(`<pi の MagicDNS 名>:5433`、DB 名 `catchup-feed`)。パスワードは Pi 側 `deploy/.env` の `POSTGRES_PASSWORD` と同じ値 |
| `RADIO_RSYNC_DEST` | `user@<pi の MagicDNS 名>:/home/<pi-user>/catchup-feed/episodes`。**Pi のホスト側パス**(pi.md 1章の `EPISODES_DIR` と同じ) |
| `RADIO_EPISODES_DIR` | `/data/episodes` 固定。DB に記録される **Pi のコンテナ内パス**(compose のマウントが対応を固定)。上と混同しない |

rsync は Tailscale ホスト名経由のみ。公開経路(radio.catchup-feed.com)にファイル転送は通さない。

## 7. Pi への ssh 鍵【ユーザー作業】

rsync(radio)とバックアップが使う。パスフレーズ無しの専用鍵でよい(tailnet 内限定):

```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_pulse -N '' -C 'pulse mac->pi'
ssh-copy-id -i ~/.ssh/id_ed25519_pulse.pub <pi-user>@<pi の MagicDNS 名>
```

`~/.ssh/config` に追記(radio の rsync と backup スクリプトが BatchMode で使えるように):

```
Host <pi の MagicDNS 名>
    User <pi-user>
    IdentityFile ~/.ssh/id_ed25519_pulse
```

確認: `ssh <pi の MagicDNS 名> 'ls -ld ~/catchup-feed/episodes'`

## 8. 手動での動作確認(launchd 登録前に必ず)

```bash
# 台本まで(TTS・転送・DB 書込なし)。話者選定・プロンプト調整もこれ(D-2)
~/pulse/bin/radio-run.sh -dry-run

# 本番一式(VOICEVOX 自動起動 → 生成 → rsync → episodes INSERT → jobs)
~/pulse/bin/radio-run.sh

# 過去記事で試したいとき
~/pulse/bin/radio-run.sh -since 2026-07-01T00:00:00+09:00
```

成功したら Pi 側で確認: `EPISODES_DIR` に mp3 があり、私的フィード(`http://<pi>:8081/private/feed.xml`)にエピソードが載り、Discord/Slack に通知が来る(有効化していれば)。

## 9. launchd(04:30 実行)+ 自動ウェイク(U-10)

```bash
cd <このリポジトリの checkout>
sed "s/CHANGEME/$(whoami)/g" deploy/launchd/com.pulse.radio.plist \
  > ~/Library/LaunchAgents/com.pulse.radio.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.pulse.radio.plist
launchctl print gui/$(id -u)/com.pulse.radio | head   # 登録確認

# 即時テスト実行(翌朝を待たずに一連を検証できる)
launchctl kickstart gui/$(id -u)/com.pulse.radio
```

失敗時の挙動: リトライしない(plist に KeepAlive を付けていない)。radio が `notify_error` ジョブを積み、Pi の worker が Discord/Slack へ通知する。ログは `~/pulse/logs/radio.{out,err}.log`。

【ユーザー作業】スリープからの自動ウェイク登録(クラムシェル運用は電源接続時のみ有効。欠番許容なので神経質にならなくてよい):

```bash
sudo pmset repeat wakeorpoweron MTWRFSU 04:25:00
pmset -g sched    # 登録確認
```

## 10. バックアップ(§8: 夜間 pg_dump を Mac へ)

radio の後(05:15)に Mac が Pi から pg_dump を pull し、episodes/ もミラーする。スクリプトは 6章で配置済み。

```bash
# 手動で1回流して確認
~/pulse/bin/backup-pulse-db.sh
ls -lh ~/pulse/backups/db/

# launchd 登録
cd <このリポジトリの checkout>
sed "s/CHANGEME/$(whoami)/g" deploy/launchd/com.pulse.backup.plist \
  > ~/Library/LaunchAgents/com.pulse.backup.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.pulse.backup.plist
```

- dump 保持は 30 日(`PULSE_BACKUP_RETENTION_DAYS`)。episodes ミラーは `--delete` なしで蓄積(約 2.2GB/年)。Pi 側は D-4 により 45 日で消えるが、ミラーには残る。
- 月次の実在確認(定常運用): `ls -lt ~/pulse/backups/db | head` で直近の dump があることを見るだけ(1分)。
- Mac が閉じていた日はバックアップもスキップ(縮退)。Pi の DB は消えていないので翌晩の dump で追いつく。

## 停止・解除(参考)

```bash
launchctl bootout gui/$(id -u)/com.pulse.radio
launchctl bootout gui/$(id -u)/com.pulse.backup
sudo pmset repeat cancel
```
