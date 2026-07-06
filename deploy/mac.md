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

【Phase 2 注意】transcribe worker(deploy/ai.md)を入れる場合、このウェイクは **02:55 に
置き換える**(pmset の繰り返しウェイクは1本しか持てない。04:30 の radio へは
transcribe-run.sh の caffeinate ブリッジで繋ぐ)。手順は ai.md 6章。

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

---

以下は Phase 2 の追加分(設計書は docs/pulse-phase2-design.md §3・§6)。
transcribe worker の導入は別ドキュメント **deploy/ai.md**。

## 11. Ollama モデル追加(Phase 2、D-12)

壁打ちと embedding のモデルを足す。**既存の qwen2.5:7b は要約フォールバック用に残す**(消さない):

```bash
ollama pull gemma4:12b     # 壁打ち用(7.6GB)。Open WebUI の既定モデルにする
ollama pull bge-m3         # embedding 用(1024次元)。book_chunks の vector(1024) と対で確定済み
ollama list                # qwen2.5:7b / gemma4:12b / bge-m3 が並ぶこと
```

- 品質が欲しい壁打ちだけ qwen3.6:27b を使う2段構えも可(D-12)。その場合も pull するだけ。
  メモリを大きく食うので**単独起動**(他モデルと同時にロードしない)が前提
- **embedding モデルの変更は重い**: Open WebUI 側の全文書再インデックスと book_chunks の
  再生成(書籍の再取り込み)を伴う。書籍を投入し始める前にモデルが確定していること(D-12 で充足)

## 12. Open WebUI(U-22)— 壁打ち UI

書籍 PDF の壁打ち(§6)の UI。Mac 上の Docker 1コンテナで動かし、スマホからは
Tailscale 経由で PWA として使う(Mac が開いている時間帯のみ使える、で許容)。

書籍・壁打ちログは私的データ(C-12)なので、**テレメトリを無効化して導入し、
リスナーは localhost に限定して tailnet 経由でだけ**開ける(LAN・インターネットに出さない)。

### 12.1 Docker(未導入の場合)

```bash
brew install --cask docker    # Docker Desktop(個人利用は無料)。初回は GUI を一度起動する
docker version                # Client/Server 両方出れば OK
```

### 12.2 コンテナ起動

```bash
docker run -d --name open-webui --restart unless-stopped \
  -p 127.0.0.1:31337:8080 \
  -e OLLAMA_BASE_URL=http://host.docker.internal:11434 \
  -e ENABLE_OPENAI_API=false \
  -e SCARF_NO_ANALYTICS=true \
  -e DO_NOT_TRACK=true \
  -e ANONYMIZED_TELEMETRY=false \
  -v open-webui:/app/backend/data \
  ghcr.io/open-webui/open-webui:main
```

- `-p 127.0.0.1:31337:8080`: **localhost バインドが分離の要**。LAN には出ない。外に見せるのは
  次節の tailscale serve だけ(pi.md 5章の「設定で分離を保証する」と同じ考え方)
- `OLLAMA_BASE_URL`: コンテナから Mac ホストの Ollama(2章)へ。macOS の Docker Desktop では
  `host.docker.internal` がそのまま解決される
- `ENABLE_OPENAI_API=false`: 外部 API 接続を切る(ゼロ円・C-12。使うのはローカル Ollama のみ)
- `SCARF_NO_ANALYTICS` / `DO_NOT_TRACK` / `ANONYMIZED_TELEMETRY`: **テレメトリ無効化(C-12 の必須条件)**
- データ(アカウント・チャット履歴・アップロード文書)は Docker ボリューム `open-webui` に残る。
  更新は `docker pull ghcr.io/open-webui/open-webui:main` → `docker rm -f open-webui` → 同じ run コマンド(冪等)
- `:main` は動くタグ(pull のたびに中身が変わる)。挙動を安定させたければ、導入時点の最新
  リリースを確認して**バージョン固定タグ(例 `:v0.x.y`)の利用を検討**し、更新は自分の
  タイミングでタグを上げる(壊れて困るのは壁打ちだけなので :main 追従でも許容範囲)

### 12.3 初期設定(ブラウザ)

1. Mac で http://127.0.0.1:31337 を開き、管理者アカウントを作成(ローカル保存。外部サービス登録ではない)
2. 既定モデル: 管理者パネル → 設定 → モデルで **gemma4:12b** を既定にする(11章で pull 済みのものが並ぶ)
3. **embedding を bge-m3 に変更**(書籍を1冊でも入れる前に必ず):
   管理者パネル → 設定 → **ドキュメント** → 「埋め込みモデルエンジン」を **Ollama**、
   モデル名を **bge-m3** にして保存(UI の文言はバージョンで多少変わる。「Documents /
   Embedding Model Engine」に相当する画面を探す)。
   既に文書を入れた後で変えた場合は、同画面の再インデックス操作(または文書の入れ直し)が必要

### 12.4 スマホから使う(Tailscale 経由 PWA)

```bash
tailscale serve --bg 31337     # https://<Mac名>.<tailnet>.ts.net → 127.0.0.1:31337
tailscale serve status        # 設定は再起動後も保持される
```

- tailscale serve が ts.net の正規 TLS 証明書で HTTPS 終端する。**PWA(ホーム画面追加)は
  HTTPS が前提**なのでこの経路を使う(3章の Ollama は TCP フォワードだったが、こちらは HTTPS プロキシ)
- スマホ(Tailscale アプリ導入済み)のブラウザで `https://<Mac名>.<tailnet>.ts.net` を開き、
  共有メニュー →「ホーム画面に追加」で PWA 化
- 到達できるのは tailnet 参加デバイスのみ。Cloudflare Tunnel には**載せない**(私的データ、C-5/C-12 と同じ分界)

### 12.5 pgvector の確認(U-24、Pi 側)

compose の postgres は `pgvector/pgvector:pg18` イメージなので拡張は同梱済み。
`CREATE EXTENSION vector` 自体は書籍 RAG のマイグレーションが自動適用する予定のため、
ここでは手動確認だけ(Pi 上で):

```bash
# 拡張が「利用可能」なこと(イメージ確認。installed_version はマイグレーション前は空で正常)
docker exec -it pulse-postgres psql -U catchup-feed -c \
  "SELECT name, default_version, installed_version FROM pg_available_extensions WHERE name='vector';"
```

### 12.6 書籍検索 Tool の登録(A-23、書籍 RAG との接続)

壁打ち中の LLM が Pi の book_chunks を検索できるようにする Tool は
catchup-feed-ai リポジトリの `openwebui/book_search_tool.py`。
登録・Valves 設定・モデルへの有効化手順は同リポジトリ README の
「Open WebUI への Tool 登録手順」を正とする。要点のみ:

- Workspace → Tools に貼り付けて Save(依存はフロントマターで自動導入)
- Valves の `DATABASE_URL` に `~/pulse/.env` と同じ DSN を設定(コンテナから Pi の
  tailnet アドレスへは Docker Desktop 経由で追加設定なしに届く)
- `OLLAMA_HOST` は既定 `http://host.docker.internal:11434`(2章の Ollama)のままでよい
- C-12: Tool の通信先はローカル Ollama と Pi の Postgres のみ

## 停止・解除(参考)

```bash
launchctl bootout gui/$(id -u)/com.pulse.radio
launchctl bootout gui/$(id -u)/com.pulse.backup
launchctl bootout gui/$(id -u)/com.pulse.transcribe   # Phase 2(ai.md)を入れている場合
sudo pmset repeat cancel
# Open WebUI(12章)を入れている場合は次の2行を**両方**実行する(&& で繋がない:
# コンテナが既に無くて docker rm が失敗しても、HTTPS 公開の解除は必ず行う)
docker rm -f open-webui
tailscale serve --https=443 off
# (`tailscale serve reset` は 3章の Ollama TCP フォワードまで消えるので使わない)
tailscale serve status   # 解除後の確認: Ollama の TCP 11434 だけが残っているのが正
```
