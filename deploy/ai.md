# transcribe worker(catchup-feed-ai)セットアップ手順(pulse Phase 2)

対象: M3 MacBook Pro(夜間バッチ)。YouTube・ポッドキャストの文字起こしを担う
transcribe worker(Python / faster-whisper)を radio と同居させる(Phase 2 設計書 §3・§5・§7)。
catchup-feed-ai はこれが**初の実デプロイ**なので、他サービスとの連携方法(7章)まで含めて書く(D-10)。

Mac が閉じている・電源が無い夜は文字起こしが**翌夜に持ち越されるだけで正常**(§5.3 縮退許容)。
リトライ装置はここには作らない — attempts 管理は jobs テーブル(worker 自身)がやる。

前提:

- mac.md 1〜10章 完了(Homebrew / Tailscale / Ollama / radio / launchd 2本が動いている)
- Pi 側が Phase 2 のコードで動いている(sources.kind マイグレーション+transcribe enqueue+§5.2b スイープ。
  更新は pi.md 3章と同じ: `docker compose -f deploy/compose.pi.yml build && docker compose -f deploy/compose.pi.yml up -d`)
- catchup-feed-ai は main マージ済み: https://github.com/Tsuchiya2/catchup-feed-ai

---

## 1. 全体像 — どこで何が動くか

transcribe worker は radio と同じ配置思想: Mac 夜間バッチ、Pi の Postgres に Tailscale 経由で直接接続、
プロセス間連携は **jobs テーブルのみ**(C-4、内部 RPC なし)。

```
┌─ Pi(常時稼働)──────────────────────────────────────────────┐
│ worker(毎時 cron)                                              │
│  ├ クロール: sources(kind=youtube|podcast)の RSS 新着検知      │
│  │   ├ youtube: 第1段 = Gemini URL 直接入力(Pi 上で完結)       │
│  │   │   成功 → 記事+要約が原子的に入る(jobs には積まれない)  │
│  │   │   失敗 → articles INSERT(content NULL)                  │
│  │   │          + jobs INSERT(kind='transcribe')  ← 原子的      │
│  │   └ podcast: 常に articles(content NULL)+ jobs enqueue      │
│  └ スイープ(§5.2b): content 有り・summary 無しの記事を要約     │
│      = Mac が夜中に埋めた文字起こしを、毎時の周回で回収する       │
└──────────────┬───────────────────────────────────┘
               │ Tailscale(Postgres 5433)
┌─ Mac(夜間 03:00〜04:15)────────────────────────────────────┐
│ pulse-transcribe(このドキュメントで導入)                       │
│  jobs を poll(kind='transcribe' 限定、SKIP LOCKED claim)        │
│   ├ youtube: 第2段 = 字幕取得 → 第3段 = yt-dlp 音声 + Whisper   │
│   └ podcast: enclosure ダウンロード + Whisper                     │
│  → articles.content に UPDATE → jobs を done に                   │
│  (音声・動画ファイルは一時ディレクトリのみ。成否によらず削除)   │
└──────────────────────────────────────────────────┘
```

content が埋まった後は**既存パイプラインが無改修で拾う**: 毎時スイープが要約 → 04:30 の radio が
記事と同列に台本へ → 放送。segments.kind も 'news' のまま(§4)。

夜間の時間割(§3。時刻はすべて JST / Mac のローカル時刻):

| 時刻 | 何が | 備考 |
|---|---|---|
| 02:55 | pmset 自動ウェイク | 6章で **04:25 から置き換える**(pmset のウェイクは1本のみ) |
| 03:00 | `com.pulse.transcribe` | 文字起こし。**04:15(`--deadline` 既定)で新規 claim 停止**、処理中のジョブは完遂 |
| 04:15〜04:32 | transcribe-run.sh のブリッジ | caffeinate で Mac を起こしたまま radio(04:30)の発火を待つ。終端は `TRANSCRIBE_BRIDGE_UNTIL`(既定 04:32) |
| 04:30 | `com.pulse.radio` | ラジオ生成(既存) |
| 05:15 | `com.pulse.backup` | pg_dump pull(既存) |
| 06:30 | (Pi)cleanup_old_media | D-4、既存 |
| 毎時 | (Pi)クロール+スイープ | 文字起こし済み記事の要約はここで追いつく |

## 2. リポジトリ配置と transcribe-run.sh

checkout は `~/pulse/` 配下に置くのを規約とする(変える場合は `~/pulse/.env` の `PULSE_AI_DIR` で指定):

```bash
git clone https://github.com/Tsuchiya2/catchup-feed-ai ~/pulse/catchup-feed-ai
```

ラッパースクリプトは backend リポジトリの deploy/ にある(launchd 資材は backend に集約):

```bash
cd <catchup-feed-backend の checkout>
cp deploy/scripts/transcribe-run.sh ~/pulse/bin/
chmod +x ~/pulse/bin/transcribe-run.sh
```

リポジトリ更新時は `git -C ~/pulse/catchup-feed-ai pull` するだけ(依存の同期は `uv run` が面倒を見る)。

## 3. uv 導入と依存同期

パッケージ管理は uv(Python 3.13 は uv が自動取得する。システムの Python は使わない):

```bash
brew install uv
cd ~/pulse/catchup-feed-ai
uv sync                      # .venv 作成 + 依存インストール(faster-whisper / yt-dlp / psycopg 等)
uv run pulse-transcribe --help   # ヘルプが出れば導入 OK(DB 接続はまだ不要)
```

## 4. 環境変数 — 既存 ~/pulse/.env に相乗りする

transcribe worker の設定は pydantic-settings(`src/pulse_transcribe/config.py`)で、
**環境変数 > カレントディレクトリの .env** の優先順で読む。運用では radio と同じ
`~/pulse/.env` を transcribe-run.sh が export するので、**設定ファイルは1つに集約される**:

- `DATABASE_URL` — **radio と完全に同じ値をそのまま共用**(mac.md 6章で設定済みのはず)。
  Pi の Postgres を tailnet 越しに指す: `postgres://catchup-feed:<POSTGRES_PASSWORD>@<pi の MagicDNS 名>:5433/catchup-feed?sslmode=disable`。
  psycopg(libpq)も Go 側と同じ URI を解釈するので書き分け不要。
  ※ catchup-feed-ai リポジトリの `.env.example` に書かれている DSN(`pulse@...:5432/pulse`)は
  汎用のプレースホルダで、**実際の値は上記(ポート 5433・DB 名 catchup-feed)が正**
- 任意キー(`WHISPER_MODEL` 等)は既定値でよければ書かなくてよい。一覧と説明は
  catchup-feed-ai の `.env.example` が正。使う場合は `~/pulse/.env` に追記する
  (env.mac.example の「transcribe worker」節に同じキーをコメントで載せてある)

リポジトリ直下(`~/pulse/catchup-feed-ai/.env`)の .env は**手動開発用**。launchd 経由では
`~/pulse/.env` の export 値が常に優先されるため、置いてあっても運用に影響しない。

## 5. 手動での動作確認(launchd 登録前に必ず)

### 5.1 接続確認(ジョブなしで起動 → 停止)

```bash
~/pulse/bin/transcribe-run.sh --deadline 23:59   # 深夜なら近い時刻に読み替える
```

`jobs: transcribe worker started` のログが出て poll に入れば DB 接続は OK。Ctrl-C で止める
(ブリッジの caffeinate が続いたらもう一度 Ctrl-C)。

**`--deadline` の意味に注意**: 「今から見て次にその時刻になる瞬間」まで動く。昼間に引数なしで
実行すると既定 04:15 は**翌朝**に解決され、朝まで poll し続ける。手動実行では必ず近い時刻を渡すこと。

なお transcribe-run.sh には**夜間窓ガード**があり、引数なし・非対話(= launchd 発火)が
02:30〜04:20 の窓外に起きた場合は何もせず正常終了する(スリープ持ち越しで昼間にまとめて
発火したとき、deadline が翌朝に解決されて丸1日走るのを防ぐ。ジョブは翌夜に持ち越し)。
手動実行は引数を渡すか端末から実行すればガードの対象外 — この章の手順どおり
`--deadline` を渡していれば意識しなくてよい。

### 5.2 実ジョブで1本通す

1. ダッシュボードで kind=youtube または podcast のソースを登録する(D-13 の選定から1本。
   YouTube の feed_url は `https://www.youtube.com/feeds/videos.xml?channel_id=...`)
2. Pi の毎時クロールを待つ(急ぐなら `docker restart pulse-worker` ではなく次の毎時発火を待つのが安全)。
   Pi 側でキューを確認:
   ```bash
   docker exec -it pulse-postgres psql -U catchup-feed -c \
     "SELECT id, status, attempts, payload->>'source_kind' AS kind, left(payload->>'media_url',60) AS url \
      FROM jobs WHERE kind='transcribe' ORDER BY id DESC LIMIT 10;"
   ```
   - **youtube でジョブが積まれない**のは第1段(Gemini URL 直接入力、Pi 上)が成功した可能性が高い
     — `summaries.provider` を見れば分かる。それも正常(jobs は第1段の失敗分だけ)
   - **ソース初回登録時**はフィード掲載分の過去エピソードがまとめて積まれることがある。
     D-14(2時間/夜)により数夜に分けて回収されるのが正常
3. Mac で実行:
   ```bash
   ~/pulse/bin/transcribe-run.sh --deadline 23:59
   ```
   - **初回は faster-whisper のモデル(large-v3-turbo、D-11)を自動ダウンロード**する。
     約 1.6GB、`~/.cache/huggingface/` 配下、無料。回線次第で数分。字幕だけで済んだ夜は
     モデルを読み込まない(遅延ロード)ので、DL が初回実行より後になることもある
   - 文字起こしは M3 の CPU で概ね実時間より速い。遅いと感じたら `~/pulse/.env` に
     `WHISPER_COMPUTE_TYPE=int8` を足す
4. 結果確認(Pi 側):
   ```bash
   # ジョブが done になり、記事に文字起こしが入っている
   docker exec -it pulse-postgres psql -U catchup-feed -c \
     "SELECT a.id, a.title, length(a.content) AS content_len \
      FROM articles a JOIN sources s ON s.id = a.source_id \
      WHERE s.kind IN ('youtube','podcast') ORDER BY a.id DESC LIMIT 5;"
   ```
5. **次の毎時サイクル後**に要約が付く(§5.2b スイープ)。worker のログに
   `summary sweep completed` が出る: `docker logs pulse-worker --since 70m | grep sweep`

## 6. launchd 登録(03:00)+ 自動ウェイクの変更【ユーザー作業あり】

**前提条件(順序厳守)**: 5章の手動確認が通っていること。特に後述のウェイク置き換えを
先にやってはいけない — transcribe-run.sh が未配置・パス誤りのまま 02:55 に切り替えると、
launchd の spawn 自体が失敗して(スクリプト内のブリッジも張られず)04:30 前に Mac が
再スリープし、**radio が発火しない**。Phase 2 の故障が Phase 1 を潰す唯一の経路がここ。

**Phase 2 の夜間運用は AC(電源)接続が前提**。caffeinate のスリープ抑止とブリッジは
バッテリー駆動では保証されない(寝た夜は文字起こし持ち越し+radio 欠番で正常、ではあるが
常態化させない)。

```bash
cd <catchup-feed-backend の checkout>
sed "s/CHANGEME/$(whoami)/g" deploy/launchd/com.pulse.transcribe.plist \
  > ~/Library/LaunchAgents/com.pulse.transcribe.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.pulse.transcribe.plist
launchctl print gui/$(id -u)/com.pulse.transcribe | head   # 登録確認

# 即時テスト(注意: 昼間に kickstart すると既定 deadline=翌朝04:15 まで動き続ける。
# 確認は 5章の手動実行で済ませ、kickstart は基本使わない)
```

【ユーザー作業】自動ウェイクを 04:25 → **02:55 に置き換える**(mac.md 9章で登録したもの):

```bash
sudo pmset repeat wakeorpoweron MTWRFSU 02:55:00
pmset -g sched    # 02:55 の1本だけになっていること
```

pmset の繰り返しウェイクは**1本しか持てない**。04:30 の radio は、transcribe-run.sh が
04:32(`TRANSCRIBE_BRIDGE_UNTIL`)まで caffeinate で Mac を起こしておく「ブリッジ」で
発火を保証する。ブリッジは EXIT trap で張られるため、.env 不在・uv 不在など**スクリプトが
どの経路で失敗してもその夜の radio は守られる**(launchd の spawn 自体の失敗だけは
救えない — 冒頭の前提条件が 5章の手動確認を要求する理由)。

失敗時の挙動: launchd はリトライしない(KeepAlive なし)。ジョブは jobs テーブルに残り、
翌夜の起動時スイープ(running の巻き戻し)とリトライ規則で勝手に回収される。
ログは `~/pulse/logs/transcribe.{out,err}.log`。スリープ持ち越しの coalesced 発火は
夜間窓ガード(5.1章)が exit 0 で受け流す(ログに "outside the night window")。

## 7. 他サービスとの連携(契約とデータの流れ)

障害時にどこを見るかはすべてこの章に帰着する。連携面は **jobs テーブル**と **articles.content** の2つだけ。

### 7.1 jobs テーブル契約(正: backend `internal/jobs` + `internal/domain/entity/job.go`)

| 項目 | 内容 |
|---|---|
| enqueue する側 | **Pi worker**。youtube/podcast の新着を articles(content NULL)+ jobs(kind='transcribe')で**原子的に** INSERT |
| claim する側 | **Mac の pulse-transcribe だけ**。`kind='transcribe'` 限定で SKIP LOCKED claim(他 kind — notify_* 等 — は Pi worker の領分で、互いに触らない) |
| payload | `{article_id, media_url, source_kind}`。source_kind は 'youtube' \| 'podcast'。キー名は両リポジトリ共通の契約(変更は破壊的) |
| status 遷移 | pending → running(claim、attempts+1)→ done / failed。リトライは pending に戻して run_after を後ろへ(attempts×1分) |
| attempts 上限 | **3**。使い切ると failed で定着(=その動画・エピソードは諦める。通知しない、ダッシュボードで観測可) |
| 起動時スイープ | Mac worker は起動時、**kind='transcribe' の running だけ**を pending に巻き戻す(前回クラッシュの孤児回収。他 kind は掃かない) |
| D-14 持ち越し | 残予算(2時間/夜)に収まらないジョブは **attempts を巻き戻して pending に返し、その夜は以降 claim せず終了**。持ち越しは失敗ではない — 何夜続いても試行の権利は減らない。**単体で2時間超**の長尺だけは即 failed(足切り) |

観測用ワンライナー(Pi 上。困ったらまずこれ):

```bash
docker exec -it pulse-postgres psql -U catchup-feed -c \
  "SELECT id, status, attempts, run_after, left(last_error,80) AS last_error \
   FROM jobs WHERE kind='transcribe' ORDER BY id DESC LIMIT 20;"
```

### 7.2 文字起こし後の要約接続(§5.2b)

Mac worker は articles.content を埋めて done にする**だけ**で、要約ジョブは積まない。
Pi worker の毎時 cron が「content 有り・summary 無し」の記事を掃き取って既存の
フォールバック連鎖(Gemini→Groq→Ollama)で要約する。kind='rss' の記事は
クロール時に記事+要約が原子的に入るため対象にならず、このスイープは実質 transcribe 経路専用。

- 要約失敗(無料枠全滅等)はその場に残して**次の毎時サイクルが自動再試行**(jobs の bookkeeping なし)
- つまり「文字起こしは done なのに要約が無い」は最長1時間の正常な過渡状態

### 7.3 縮退挙動(§5.3。どれも「翌日勝手に戻る」が仕様)

| 障害 | 挙動 | 観測 |
|---|---|---|
| Mac 不在の夜 | 文字起こし持ち越し。その日のラジオは RSS 記事のみで成立 | jobs に pending が残る |
| Gemini 全滅(第1段) | 第2段・第3段が翌夜 Mac で回収 | jobs に enqueue される |
| 字幕なし+Whisper 失敗 | attempts 3 で failed、その動画は諦める | `status='failed'`、last_error |
| 単体で2時間超の長尺 | 即 failed(足切り。処理を試みない) | last_error に "can never fit" |
| 残予算に収まらない | attempts を消費せず pending に戻し、その夜は終了 | ログ "job deferred" |
| DB / Tailscale 断 | worker は起動失敗 or claim 失敗ログ。ジョブは無傷で翌夜へ | transcribe.err.log |

## 8. トラブルシューティング

- **Postgres に繋がらない**(`connection refused` / タイムアウトで即死)
  1. Tailscale が動いているか: `tailscale status`(メニューバーのアプリが落ちていることがある)
  2. Pi に届くか: `ping <pi の MagicDNS 名>` / `nc -z <pi の MagicDNS 名> 5433`
  3. Pi 側のコンテナ: `docker compose -f deploy/compose.pi.yml ps`(pi.md トラブル節)
  4. どれでもなければ `~/pulse/.env` の DATABASE_URL(radio が動いているなら値は正しいはず)
- **モデルのダウンロード失敗**(初回のみ。Hugging Face への到達性)
  ネットワーク回復後に再実行するだけ(部分DLは再開される)。ジョブは失敗しても attempts 3 の
  範囲でリトライされるので、DL 成功後の周回で回収される。手動で先に落としておくなら
  5章の実ジョブ確認を昼間にやっておく
- **yt-dlp が YouTube 側の変更で失敗する**(これは定期的に起きる)
  - まず知っておくこと: **attempts 3 で failed になり、翌日は新しい動画で回復するのが正常挙動**。
    その動画を無理に救済する装置は作らない(縮退許容)
  - 続くようなら yt-dlp を上げる: `cd ~/pulse/catchup-feed-ai && uv lock --upgrade-package yt-dlp && uv sync`
    (yt-dlp は YouTube 側変更への追随が速い。上げても直らない期間は字幕がある動画だけが通る)
- **朝になっても文字起こしが無い**
  1. そもそも走ったか: `~/pulse/logs/transcribe.out.log` の日付。
     "outside the night window" があれば、Mac がスリープ中に 03:00 を跨いで蓋開け時に
     まとめて発火したケース — 窓ガードのスキップで正常(翌夜に持ち越し)
  2. 走ったが空振り: "transcribe worker started" 後に claim が無い → 7.1 の SQL で pending の有無を確認。
     pending ゼロなら第1段(Gemini)で全部済んでいる(正常)
  3. deferred で終了している → D-14 の持ち越し(正常)。連夜続くなら backlog が2時間/夜を
     超えている — ソースを減らすか `NIGHTLY_BUDGET_SECONDS` を調整(deadline 04:15 は不変なので上げ過ぎ注意)
- **文字起こしはあるが要約が付かない** → 7.2。1時間待つ。それでも付かないなら
  `docker logs pulse-worker --since 70m | grep -i sweep` と `summaries.provider`(pi.md トラブル節)

## 9. 書籍 PDF RAG(将来追記)

書籍取り込み(PDF → チャンク化 → embedding → Pi の pgvector)は実装ステップ7で
このドキュメントに追記する。壁打ち UI(Open WebUI)と Ollama モデル(D-12)の導入は
mac.md 11〜12章、pgvector の確認は mac.md 12章を参照。

## 停止・解除(参考)

```bash
launchctl bootout gui/$(id -u)/com.pulse.transcribe
sudo pmset repeat wakeorpoweron MTWRFSU 04:25:00   # ウェイクを radio 単独運用(mac.md 9章)に戻す
```
