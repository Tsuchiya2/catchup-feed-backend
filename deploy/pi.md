# Pi 5 セットアップ手順(catchup-feed Phase 1)

対象: Raspberry Pi 5(常時稼働)。server + worker + PostgreSQL 18 + mp3 アーカイブを載せる(設計書 §3)。

**ステータス(2026-08-15 現在)**: 本書はもともと初代 catchup-feed スタックとの**共存**を前提に書かれていたが、
初代の停止は **2026-07-06 に完了**し、docker 資産・systemd unit・Cloudflare の旧ホスト名も撤去済み
(legacy-shutdown.md)。Pi で動いているのは pulse だけである。**一回限りの移行手順である 3.5章と10章**は
いずれも実行対象が無い履歴なので、新規セットアップでは読み飛ばしてよい(章冒頭の注記を読むこと)。
**名前の重なりへの注意喚起は現役** — compose
プロジェクト名・ディレクトリ名・unit 名・コンテナ名がすべて `catchup-feed` 系で重なるため、
停止・削除系のコマンドは今も打つ前に実体を確認する(legacy-shutdown.md 8章「地雷」)。

前提(既に済んでいるもの): Docker + docker compose plugin、Tailscale 参加済み、cloudflared が稼働中
(初代が使っていた既存トンネルを pulse がそのまま引き継いでいる。5章)。

表記: `<pi-user>` = Pi のログインユーザー。以下のパスは好みで変えてよいが、変えた場合は `.env` と Mac 側設定も揃えること。

---

## 1. ディレクトリと mp3 アーカイブの用意

```bash
mkdir -p ~/catchup-feed/episodes
# コンテナ(uid/gid 10001 の非 root ユーザー)が読み書きできるようにする:
#   - server は配信のため読み取り、worker は D-4 cleanup のため削除(=ディレクトリ書込)が必要
#   - setgid(2xxx)により Mac からの rsync で置かれるファイルも gid 10001 を継承する
sudo chgrp 10001 ~/catchup-feed/episodes
sudo chmod 2775 ~/catchup-feed/episodes

# 書籍 PDF の置き場(D-25)。server がアップロード保存・削除で読み書きする
mkdir -p ~/catchup-feed/books
sudo chgrp 10001 ~/catchup-feed/books
sudo chmod 2775 ~/catchup-feed/books
```

## 2. リポジトリ配置と .env 作成

```bash
cd ~/catchup-feed
git clone <このリポジトリ> catchup-feed-backend
cd catchup-feed-backend
cp deploy/env.pi.example deploy/.env
chmod 600 deploy/.env
```

`deploy/.env` を編集して値を埋める。**秘密の値はファイルに直接記入し、チャット等に貼らない**。必須キーと生成コマンド:

| キー | 入れるもの |
|---|---|
| `TAILNET_IP` | `tailscale ip -4` の出力 |
| `EPISODES_DIR` | `/home/<pi-user>/catchup-feed/episodes`(1章で作った絶対パス) |
| `BOOKS_DIR` | `/home/<pi-user>/catchup-feed/books`(1章で作った絶対パス、D-25) |
| `POSTGRES_PASSWORD` | `openssl rand -base64 24` |
| `JWT_SECRET` | `openssl rand -base64 48`(U-3) |
| `ADMIN_PASSWORD_HASH` | `make admin-hash` の出力。**`$` は `$$` にエスケープ**(U-3) |
| `GEMINI_API_KEY` / `GROQ_API_KEY` | U-4 で取得した値 |
| `OLLAMA_HOST` | `http://<Mac の Tailscale IP>:11434`(Mac 上で `tailscale ip -4`。mac.md 3章の後で)。**MagicDNS 名は不可**(Ollama の Host 検証が `.ts.net` を 403 で拒否。mac.md 3章参照) |
| `SMTP_ENABLED` / `SMTP_HOST` / `SMTP_PORT` / `SMTP_USERNAME` / `SMTP_PASSWORD` / `SMTP_FROM` | メール通知(本人向け D-29。友人向けメールは D-32 で廃止)。Gmail なら `SMTP_HOST=smtp.gmail.com`・`SMTP_PORT=587`・`SMTP_USERNAME=<Gmail アドレス>`・`SMTP_PASSWORD=<アプリパスワード>`(U-11: 2 段階認証を有効にして [Google アカウント > セキュリティ > アプリパスワード] で発行)。`SMTP_FROM` は未設定なら `SMTP_USERNAME`。使う段階で `SMTP_ENABLED=true`。現用は旧システムの Gmail アカウント(Pi の `~/.msmtprc` と同一資格情報)を流用(D-30-1)。無効化されたら U-11 の手順で再発行 |
| `NOTIFY_ERROR_EMAIL_TO` | 本人向け通知(notify_error の障害通知+新着エピソード通知)の宛先アドレス(D-29)。`SMTP_ENABLED=true` が前提。空なら本人向け通知は送られない |

DB は**専用の PostgreSQL サーバー**を持つ(`catchup-feed-postgres` コンテナ、database 名 `catchup-feed`、ホスト側ポート 5433)。初代 catchup-feed の DB(`catchup-postgres`、ハイフンの後が違うだけの別コンテナ)とはサーバーごと分離してあり、**初代側は 2026-08-15 に撤去済みで Pi 上に実体は無い**。**したがって現在この Pi にある `catchup*` はすべて pulse の現用資産**である — 古い手順書やメモに残る `catchup-postgres` 等の名前を見て「初代の残骸だろう」と `catchup-feed-postgres` を落とさないこと(legacy-shutdown.md 8章「地雷」)。初代 DB からデータは移行していない — sources 定義は `internal/infra/db/seeds/sources.sql` が server の**初回起動時(sources テーブルが0行のとき)のみ**自動投入される。2回目以降の起動では再投入されない(ダッシュボードで削除したソースが再起動で復活しないようにするため)。

## 3. ビルドと起動

Pi の compose は **ルート `compose.yml`(ベース)+ `deploy/compose.pi.yml`(Pi 固有 override)の 2 枚重ね**。
本書のすべての compose コマンドは次の形で、**必ずリポジトリルートから**実行する:

```bash
docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env <サブコマンド>
```

`--env-file deploy/.env` は省略不可。複数 `-f` 指定時の project directory は最初の `-f` の
ディレクトリ(= リポジトリルート)になるため、`deploy/.env` は自動では読まれない
(ルートに開発用 `.env` があるとそちらが読まれてしまう)。毎回打つのが面倒なら
`~/.bashrc` に alias を足してよい(以降の例は素の形で書く):

```bash
alias dcpi='docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env'
```

```bash
cd ~/catchup-feed/catchup-feed-backend
docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env build   # Pi ネイティブ arm64 ビルド。初回は時間がかかる
docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env up -d
docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env ps      # 3コンテナとも healthy になること
```

マイグレーション(§4 スキーマ)は `server` の起動時に毎回自動適用される。sources シードは sources テーブルが空のとき(初回セットアップ)のみ投入される。専用コマンドは無い。

## 3.5. compose プロジェクト名リネーム(`pulse` → `catchup-feed`)の移行手順【履歴・実行対象なし】

> **この章に実行対象はもう無い(2026-08-15 の棚卸しで確認)。以下は当時の記録。**
> 現行 Pi はリネーム済みで、`pulse-*` コンテナ・`pulse_db-data` ボリュームは残っていない。
> 新規 Pi のセットアップでもこの章は不要 — 3章のまま `up -d --build` でよい。

過去に compose プロジェクト名 `pulse`(コンテナ `pulse-*`、ボリューム `pulse_db-data`)で稼働していた
Pi を、現行の `name: catchup-feed`(コンテナ `catchup-feed-*`、ボリューム `catchup-feed_db-data`)へ
移行するための手順だった。**データ喪失を許容する前提**(新プロジェクトは空ボリュームで起動し、sources は
初回起動時に seeds が投入する)。

### precondition(当時の判断基準。**今そのまま実行しないこと**)

新プロジェクト名 `catchup-feed` が**初代 catchup-feed の compose プロジェクト**とぶつからないことを、
`up` の前に確認する手順だった。初代のプロジェクト・ボリュームが残っていれば同名衝突や意図しない
ボリューム再利用が起きうる、という想定である。

**この確認を今日打つと必ず「衝突あり」と判定される** — `docker compose ls -a` に出てくる `catchup-feed`
も `catchup-feed_*` ボリュームも**現行 pulse 自身**だからである(初代は 2026-07-06 に停止し、docker 資産は
2026-08-15 に撤去済み。legacy-shutdown.md 8章で実測確認)。ここで「衝突あり」と読んで撤去手順
(legacy-shutdown.md 6章)へ進むと、pulse の DB ボリュームを消すことになる。

```bash
docker compose ls -a                    # 出てくる catchup-feed は現行 pulse(-a で停止中も出す)
docker volume ls | grep catchup-feed    # catchup-feed_db-data も現行 pulse のもの
```

### 手順(当時)

1. **旧プロジェクトを明示的に落とす**。compose ファイルを編集した後は、`docker compose` は
   新プロジェクト名 `catchup-feed` で動くため、`-p pulse` を付けないと旧 `pulse-*` コンテナ・
   `pulse_db-data` ボリュームを掴めない。

   ```bash
   # データを引き継ぎたい場合は、落とす前にここで退避(任意)
   docker exec pulse-postgres pg_dump -U catchup-feed -Fc catchup-feed > /tmp/pre-rename.dump

   docker compose -p pulse -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env down
   ```

2. **新プロジェクトで起動**。新ボリューム `catchup-feed_db-data`(空)が作られ、**DB は初期状態**に
   なる。sources は server の初回起動時(テーブルが空のとき)に seeds が投入する。

   ```bash
   docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env up -d --build   # プロジェクト名は name: catchup-feed
   docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env ps              # catchup-feed-* が healthy に
   ```

3. **データを引き継ぐ場合(任意)**。1 で取った dump を新スタックへ流し込む。

   ```bash
   docker exec -i catchup-feed-postgres pg_restore -U catchup-feed -d catchup-feed \
     --clean --if-exists < /tmp/pre-rename.dump
   ```

旧 `pulse_db-data` ボリュームは 2 の起動後も残る(自動削除されない)。引き継ぎ確認が済んだら
`docker volume rm pulse_db-data` で回収してよい。

> 注: systemd unit 名は `pulse.service` のまま(改名しない)。unit は compose の
> `up -d` を呼ぶだけで、compose プロジェクト名とは独立。据え置いた理由は初代の
> `catchup-feed.service` と衝突させないためで、初代 unit 自体は 2026-08-15 に削除済み
> (legacy-shutdown.md 3章)。それでも改名しない — 名前を `catchup-feed.service` に寄せると
> 初代の手順書・ログとの区別が付かなくなるため(4章の注意も参照)。

## 4. systemd による常時稼働化

コンテナ自体は `restart: unless-stopped` で自己回復するが、**ブート時だけは順序が要る**: `TAILNET_IP` へのポートバインドは tailscaled が IP を持った後でないと失敗し、Docker の再起動ポリシーは「一度も起動に成功していないコンテナ」を再試行しない。そこで tailscaled 起動後に compose を実行する oneshot ユニットを入れる。

ユニットは `up -d --force-recreate --remove-orphans` で**毎起動コンテナを作り直す**(D-28)。ブレーカー断などの不正シャットダウンで残る壊れたコンテナ状態(2026-07-22 障害ではネットワーク接続喪失のまま再起動ループ)を構造的に捨てるため。起動失敗時は systemd が失敗のたび 60 秒待機してから再試行する(最大 5 回/1 時間、無限にはしない)。docker.service への依存は `Wants=` + ExecStartPre の `docker info` 待ちで表現しており、docker 未準備も「自ユニットの失敗」としてこのリトライの傘に入る。

```bash
# WorkingDirectory を実パスに書き換えてから配置
sed "s|/home/CHANGEME/pulse|$HOME/catchup-feed|" deploy/systemd/pulse.service | \
  sudo tee /etc/systemd/system/pulse.service >/dev/null
sudo systemctl daemon-reload
sudo systemctl enable --now pulse.service
systemctl status pulse.service   # active (exited) なら正常
```

注意: 初代の `catchup-feed.service` とは**別 unit**。初代 unit は毎起動失敗していたため D-28 (3) で `systemctl disable` し、2026-08-15 にファイルごと削除 + `reset-failed` 済み(legacy-shutdown.md 3章・8章)。**現在 Pi にある pulse 関連の unit は `pulse.service` だけ**なので、`catchup-feed.service` という名前が再び現れたらそれは初代の復活か設定ミスである。

## 5. Cloudflare Tunnel — ルート追加【ユーザー作業】(U-9)

catchup-feed が公開するのは `radio.catchup-feed.com` → `127.0.0.1:8090`(公開リスナー)だけ。設定例と「公開してよいルート」の一覧は `deploy/cloudflared/config.example.yml` に記載。

> 注: **現用の Tunnel は remote managed**(Cloudflare ダッシュボードの Public Hostname が正)。
> Pi の `/etc/cloudflared/config.yml` にも ingress が書かれているが**実効設定ではない**ため、
> ホスト名の追加・削除はダッシュボード側で行う。`config.example.yml` は config ファイル運用の
> 場合の例であり、これだけを見ると「ローカルファイルが正」と誤解する。さらに現在のローカル
> ingress は陳腐化しており、`radio.catchup-feed.com` のエントリが無く初代由来の
> `grafana` / `prometheus` が残っている。**この状態で config ファイル運用に戻すとフィード配信が
> 404 に落ちる**。戻す必要が出たときは、先にダッシュボードの Public Hostname 一覧を
> ローカル ingress へ写し取ること。

1. DNS: 既存トンネルに向ける
   ```bash
   cloudflared tunnel route dns <既存トンネル名> radio.catchup-feed.com
   ```
   (またはCloudflare ダッシュボードの DNS / Zero Trust → Tunnels → Public Hostname で追加)
2. Ingress: config ファイル運用なら `/etc/cloudflared/config.yml` の ingress に
   `hostname: radio.catchup-feed.com → service: http://localhost:8090` を追記し、
   `sudo systemctl restart cloudflared`。ダッシュボード管理のトンネルなら Public Hostname 追加のみで完了。
3. 初代システム向けのルートは 2026-07-11 に、初代ダッシュボードの `catchup.catchup-feed.com` は **2026-08-15 に削除済み**(legacy-shutdown.md 4章)。**`catchup-feed.com` 配下で現用のホスト名は `radio.catchup-feed.com`(フィード配信)と `pulse.catchup-feed.com`(ダッシュボード)の2つだけ**で、これ以外が Public Hostname や DNS に残っていたら初代の残骸なので消してよい(`AUTH_COOKIE_DOMAIN=.catchup-feed.com` はワイルドカードなので、使っていないホスト名にも認証クッキーが飛ぶ)。
4. **レートリミットの前提(Tunnel 経由の公開では必須)**: `deploy/.env` に
   `RATE_LIMIT_TRUST_PROXY=true` と `RATE_LIMIT_TRUSTED_PROXIES=127.0.0.1/32`
   が入っていること(env.pi.example の既定値。2章で写していれば設定済み)。
   cloudflared は Pi 上の 127.0.0.1 で終端するため、X-Forwarded-For を信頼
   しないと全リクエストが 127.0.0.1 に見え、公開ルートの per-IP 制限
   (無効トークン連打対策)が効かない。信頼先はローカルの cloudflared だけ
   なので詐称の懸念はない。**Tunnel をやめて server を直接公開する構成に
   変える場合は false に戻す**(XFF をクライアントが自由に詐称できるため)。

### 公開/私的の分離が設定で保証される理由(必ず理解してから公開する)

- Tunnel が繋がる先は `127.0.0.1:8090` = server の**公開リスナーのみ**。ここには `/feeds/{token}/*`(トークン認証)、`/auth/token`、JWT 必須の管理 API しか載っていない。
- 私的フィード(`/private/*`)は**別リスナー**で、compose がホスト側 `${TAILNET_IP}:8081` にしか公開しない。LAN にもインターネットにも出ない(C-5: 物理境界が認証)。
- したがって「Tunnel の設定ミスで私的フィードが漏れる」経路が構造上存在しない。漏れうる唯一の設定ミスは compose の ports を書き換えることだけ。

## 6. Mac からの rsync / ssh 受け入れ

radio バッチ(Mac)は `rsync over ssh` で mp3 を `EPISODES_DIR` に置き、バックアップスクリプトも同じ ssh 経路を使う。

1. 【ユーザー作業】Mac の公開鍵を Pi の `~/.ssh/authorized_keys` に登録(mac.md 7章で生成)
2. 動作確認(Mac 側から):
   ```bash
   ssh <pi-user>@<pi の MagicDNS 名> 'ls -ld ~/catchup-feed/episodes'
   ```

rsync は **Tailscale の MagicDNS 名**を使う(公開経路にファイル転送を通さない)。`RADIO_RSYNC_DEST` の宛先パスは**ホスト側**の `EPISODES_DIR`、DB に記録されるパス(`RADIO_EPISODES_DIR`)は**コンテナ内**の `/data/episodes`。この対応は compose のマウント `${EPISODES_DIR}:/data/episodes` が固定している。

## 7. 動作確認

```bash
# ヘルスチェック
curl -s http://127.0.0.1:8090/health

# ログイン → 管理 API(トークン発行はダッシュボードから行うのが本線)
# JSON のキーは "email"(値は .env の ADMIN_USER と一致させる)
curl -s -X POST http://127.0.0.1:8090/auth/token -d '{"email":"<ADMIN_USER の値>","password":"<平文パスワード>"}'

# worker がクロールしているか
docker logs catchup-feed-worker --since 10m

# 公開フィード(トークン発行後、tailnet 外 = スマホのモバイル回線から)
#   https://radio.catchup-feed.com/feeds/<token>/feed.xml → 200
#   https://radio.catchup-feed.com/private/feed.xml       → 401(分離の検証)
#     ※ 401 は「認証付きで公開されている」わけではない。公開リスナーに
#       /private ルートは存在せず、デフォルトの JWT 保護ハンドラ("/")に
#       落ちて 401 になるだけ。私的フィード実体は tailnet 側リスナーのみ。
#   https://radio.catchup-feed.com/feeds/deadbeef/feed.xml → 401/404(無効トークン)

# 私的フィード(tailnet 内から)
curl -s http://<pi の MagicDNS 名>:8081/private/feed.xml
```

エピソードが載るのは Mac 側(mac.md)が動いた翌朝から。日次フロー(§3.3、コードで確認済みの順序): radio が rsync で mp3 を置く → **成功後に** episodes/segments を INSERT → jobs に `regenerate_feed`(現実装ではフィードは毎リクエスト生成のため no-op)と `notify_episode` を積む → worker が通知。フィードに「実体のない mp3」が載る瞬間はない。

## 8. リストア手順(四半期のリストア試験もこれで)

バックアップは Mac 側に日次で取られる(mac.md 10章)。戻すとき:

```bash
# Mac から dump を Pi へ(tailnet 経由)。ファイル名は backup-pulse-db.sh の形式
scp ~/pulse/backups/db/pulse-<日時>.dump <pi-user>@<pi の MagicDNS 名>:/tmp/

# Pi 側で(試験時は catchup-feed_restore_test など別 DB 名にすること)
docker exec -i catchup-feed-postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists' < /tmp/pulse-<日時>.dump
rm /tmp/pulse-<日時>.dump
```

mp3 は Mac 側ミラー(`~/pulse/backups/episodes/`)から `EPISODES_DIR` へ rsync で戻す。

## 9. Pi ローカル5分監視(D-30-2)

`deploy/scripts/pi-health-check.sh` が正。cron(5分ごと)で3コンテナの docker health と
server のローカル HTTP 応答(127.0.0.1:8090、2xx/4xx を正常とみなす)をチェックし、
**状態遷移時のみ** msmtp(`~/.msmtprc` の account gmail、旧システムから流用 D-30-1)で
メールする。宛先はリポジトリに書かず、`deploy/.env` の `NOTIFY_ERROR_EMAIL_TO`
(または環境変数 `PULSE_HC_MAIL_TO`)からスクリプトが実行時に読む。未設定なら
送信せずログにエラーを残す。正常→異常は連続2回で「障害検知」1通、
異常→正常で「復旧」1通。連続異常中は再送しない(旧 health-check.sh のスパム問題の修正)。
メール送信に失敗した回は状態遷移せず、次回実行で再送する。
pulse.service 起動から5分間は異常判定を保留(起動直後の誤検知対策)。

配置とセットアップ(冪等。スクリプト更新時も同じ手順):

```bash
# 1) 配置(checkout 内を cron から直接実行しない。~/bin にコピーが実体)
mkdir -p ~/bin
install -m 755 ~/catchup-feed/catchup-feed-backend/deploy/scripts/pi-health-check.sh ~/bin/pi-health-check.sh

# 2) ログ置き場(状態ファイル pi-health-check.state もここ)
sudo mkdir -p /var/log/catchup && sudo chown ubuntu:ubuntu /var/log/catchup

# 3) logrotate(遷移時のみ追記なので通常は増えないが保険)
sudo tee /etc/logrotate.d/pulse-health-check >/dev/null <<'CONF'
/var/log/catchup/pi-health-check.log {
    monthly
    rotate 3
    size 1M
    missingok
    notifempty
    compress
    su ubuntu ubuntu
}
CONF

# 4) cron 登録(既存エントリがあれば入れ替え)
( crontab -l 2>/dev/null | grep -v "pi-health-check.sh" | grep -v "^# pulse Pi 死活監視" ;
  echo "# pulse Pi 死活監視(D-30-2)- 5分ごと、状態遷移時のみメール送信" ;
  echo "*/5 * * * * /home/ubuntu/bin/pi-health-check.sh >> /var/log/catchup/pi-health-check.log 2>&1" ) | crontab -

# 5) 動作確認: 正常時は何も送らない(state=OK になるだけ)
~/bin/pi-health-check.sh && cat /var/log/catchup/pi-health-check.state
# 異常系まで試すなら(worker を短時間止める。server は止めない):
#   PULSE_HC_SUBJECT_PREFIX="[TEST] " を付けて実行すると件名で実障害と区別できる
#   docker stop catchup-feed-worker → スクリプト2回実行(2回目で障害メール)
#   → docker start catchup-feed-worker → healthy 後に1回実行(復旧メール)
```

Mac 側の朝チェック(mac.md、05:45 の morning-check)が外からの死活確認、
この5分監視が Pi 内部からの検知。両方ともメール経路は Gmail SMTP のみで、
Cloudflare Tunnel / 公開面には何も追加しない。

## 10. Pi での切替手順(単一ファイル構成 → override 構成)【履歴・実行対象なし】

> **この章に実行対象はもう無い。** 現行 Pi は 2 枚重ね構成へ切替済みで、以下は当時の記録。
> 新規 Pi のセットアップには不要(3章のとおり最初から 2 枚重ねで起動する)。

compose.pi.yml が単体で完結していた構成(`docker compose -f deploy/compose.pi.yml ...`)で
稼働中の Pi を、本書 3 章の 2 枚重ね構成へ切り替えた手順。プロジェクト名(`catchup-feed`)・
コンテナ名・ポート・ボリュームはすべて不変なので **DB・mp3 に影響はない**。レンダリング結果の
差分は pgvector イメージのピン(`pg18` → `0.8.5-pg18`。実体は同じ PG 18 系)のみ。

```bash
cd ~/catchup-feed/catchup-feed-backend

# 0) pgvector ピン(0.8.5)がダウングレードにならないことの事前確認(停止前に実行)。
#    0.8.5 以下なら安全。超えていたら compose.yml のピンを実バージョンに合わせてから進む
docker exec catchup-feed-postgres psql -U catchup-feed -d catchup-feed \
  -c "select extversion from pg_extension where extname='vector';"

# 1) 旧構成のまま停止(git pull の前に行う。pull 後の compose.pi.yml は
#    override 専用になり単体では -f 指定できないため。pull を先にしてしまった
#    場合は代わりに `docker compose -p catchup-feed down` でプロジェクト名指定で落とす。
#    それも :? の補間エラーで失敗する場合は、compose ファイルの無いディレクトリに
#    cd してから同コマンドを実行する(ラベルベースで停止できる)
docker compose -f deploy/compose.pi.yml down

# 2) 取り込み
git pull

# 3) 新コマンドで起動(pgvector イメージの pull が入る)
docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env up -d --build
docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env ps   # 3コンテナ healthy

# 4) systemd unit を差し替え(ExecStart が新コマンドに変わっている。
#    WorkingDirectory はリポジトリ「ルート」に変更された点に注意)
sed "s|/home/CHANGEME/pulse|$HOME/catchup-feed|" deploy/systemd/pulse.service | \
  sudo tee /etc/systemd/system/pulse.service >/dev/null
sudo systemctl daemon-reload
systemctl cat pulse.service | grep -E 'WorkingDirectory|ExecStart'   # 目視確認

# 5) 次回ブートを待たずに unit 経由の起動も確認しておく(D-28 の復旧経路の検証)
sudo systemctl restart pulse.service
systemctl status pulse.service   # active (exited) なら正常
```

切替後の動作確認は 7 章と同じ(health・公開フィード・私的フィード・worker ログ)。

## 11. 運用 Tips

- **build cache の掃除**: `build` を繰り返すと Docker の build cache が数 GB 単位で溜まる。
  稼働中のコンテナと現用イメージには影響しないので、ディスクが逼迫したら回収してよい。

  ```bash
  docker system df       # Build Cache 行の RECLAIMABLE を確認
  docker builder prune   # 確認プロンプトあり(全キャッシュを消すなら -a)
  ```

  2026-08-15 の棚卸しでは build cache 4.1GB のうち 3.3GB を回収し、**ディスク使用率が 43% → 32%**
  になった。次回ビルドはキャッシュ消失分だけ遅くなるが、Pi ネイティブ arm64 ビルドでも実用範囲。
- 使われていないイメージは `docker image prune -a`(使用中のイメージは消えない)。ただし
  **直前のビルドのイメージ(ロールバック用)と golang ビルドベースも消える**ため、
  `docker builder prune` と併用すると次回は完全なフルビルドになる。ロールバック先を残したい
  ときはタグ無しイメージだけを落とす `docker image prune`(`-a` なし)に留める。
- 初代 catchup-feed の残骸(systemd unit・logrotate・旧バックアップ・docker 資産・Cloudflare の
  旧ホスト名)は 2026-08-15 までにすべて撤去済み。棚卸しの結果と**残る確認項目**は
  `legacy-shutdown.md` 8章のチェックリストが正。

## トラブル時の見方(監視スタックは無い。これで足りる)

- コンテナ状態: `docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env ps` / `docker logs catchup-feed-server` / `docker logs catchup-feed-worker`
- 要約フォールバックの発生: `summaries.provider` を見る(`docker exec -it catchup-feed-postgres psql -U catchup-feed -c "select provider, count(*) from summaries group by 1"`)
- 朝エピソードが無い日: 正常系の欠番(Mac 不在)か、radio の失敗通知(notify_error のメール、D-29)かをまず確認
- `systemctl --failed` に出る `fwupd.service` / `fwupd-refresh.service` / `logrotate.service` は **OS 由来で pulse とは無関係**(pulse 側の unit は `pulse.service` だけ)。`logrotate.service` は `/var/log/unattended-upgrades/*.log` を読めず Permission denied で落ちる Ubuntu 側の既知問題。これらのせいで `systemctl is-system-running` は恒常的に `degraded` を返すので、**`degraded` 単体を障害の根拠にしない**(必ず `systemctl --failed` の unit 名まで見る)。ここに `catchup-feed.service` など初代の unit が出ていたら legacy-shutdown.md 3章の `reset-failed` を打つ
