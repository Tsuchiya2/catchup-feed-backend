# Pi 5 セットアップ手順(catchup-feed Phase 1)

対象: Raspberry Pi 5(常時稼働)。server + worker + PostgreSQL 18 + mp3 アーカイブを載せる(設計書 §3)。
旧 catchup-feed スタックとは **§9 の停止手順まで共存**する。コンテナ名・ポート・DB はすべて分離済みなので、旧側には触らない。

前提(既に済んでいるもの): Docker + docker compose plugin、Tailscale 参加済み、旧システム用の cloudflared が稼働中。

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
| `POSTGRES_PASSWORD` | `openssl rand -base64 24` |
| `JWT_SECRET` | `openssl rand -base64 48`(U-3) |
| `ADMIN_PASSWORD_HASH` | `make admin-hash` の出力。**`$` は `$$` にエスケープ**(U-3) |
| `GEMINI_API_KEY` / `GROQ_API_KEY` | U-4 で取得した値 |
| `OLLAMA_HOST` | `http://<Mac の Tailscale IP>:11434`(Mac 上で `tailscale ip -4`。mac.md 3章の後で)。**MagicDNS 名は不可**(Ollama の Host 検証が `.ts.net` を 403 で拒否。mac.md 3章参照) |
| `DISCORD_WEBHOOK_URL` / `SLACK_WEBHOOK_URL` | U-7 で取得した値(使う側の `*_ENABLED=true` も) |
| `SMTP_*` | U-8 で取得した値(友人メール通知を使う段階で) |

旧 catchup-feed の DB とは **PostgreSQL サーバーごと分離**する(このスタックは専用の `catchup-feed-postgres` コンテナ、database 名 `catchup-feed`、ホスト側ポート 5433)。**旧システムの `catchup-postgres`(ハイフンの後が違うだけの別コンテナ)と取り違えない**。旧 DB からデータは移行しない — sources 定義は `internal/infra/db/seeds/sources.sql` が server 起動時に自動投入される(冪等、`ON CONFLICT DO NOTHING`)。

## 3. ビルドと起動

```bash
cd ~/catchup-feed/catchup-feed-backend
docker compose -f deploy/compose.pi.yml build     # Pi ネイティブ arm64 ビルド。初回は時間がかかる
docker compose -f deploy/compose.pi.yml up -d
docker compose -f deploy/compose.pi.yml ps        # 3コンテナとも healthy になること
```

マイグレーション(§4 スキーマ)と sources シードは `server` の起動時に自動適用される。専用コマンドは無い。

## 3.5. compose プロジェクト名リネーム(`pulse` → `catchup-feed`)の移行手順

過去に compose プロジェクト名 `pulse`(コンテナ `pulse-*`、ボリューム `pulse_db-data`)で稼働していた
Pi を、現行の `name: catchup-feed`(コンテナ `catchup-feed-*`、ボリューム `catchup-feed_db-data`)へ
移行する場合の手順。**データ喪失を許容する前提**(新プロジェクトは空ボリュームで起動し、sources は
seeds が冪等投入する)。まだ稼働していない新規 Pi ならこの節は不要で、3章のまま `up -d --build`。

### precondition(必ず `up` 前に確認)

新プロジェクト名 `catchup-feed` が、**旧 catchup-feed compose プロジェクト**(名前が衝突しうる)と
ぶつからないことを確認する。旧システム(§9)のプロジェクト・ボリュームが残っていると、同名衝突や
意図しないボリューム再利用が起きうる。

```bash
docker compose ls                       # catchup-feed という名の別プロジェクトが無いこと
docker volume ls | grep catchup-feed    # 旧由来の catchup-feed_* ボリューム/ネットワークが無いこと
```

衝突しうるものが残っている場合は、先に legacy-shutdown.md(§9 / U-15)の停止・撤去を済ませる。

### 手順

1. **旧プロジェクトを明示的に落とす**。compose ファイルを編集した後は、`docker compose -f ...` は
   新プロジェクト名 `catchup-feed` で動くため、`-p pulse` を付けないと旧 `pulse-*` コンテナ・
   `pulse_db-data` ボリュームを掴めない。

   ```bash
   # データを引き継ぎたい場合は、落とす前にここで退避(任意)
   docker exec pulse-postgres pg_dump -U catchup-feed -Fc catchup-feed > /tmp/pre-rename.dump

   docker compose -p pulse -f deploy/compose.pi.yml down
   ```

2. **新プロジェクトで起動**。新ボリューム `catchup-feed_db-data`(空)が作られ、**DB は初期状態**に
   なる。sources は server 起動時に seeds が冪等投入する。

   ```bash
   docker compose -f deploy/compose.pi.yml up -d --build   # プロジェクト名は name: catchup-feed
   docker compose -f deploy/compose.pi.yml ps              # catchup-feed-* が healthy に
   ```

3. **データを引き継ぐ場合(任意)**。1 で取った dump を新スタックへ流し込む。

   ```bash
   docker exec -i catchup-feed-postgres pg_restore -U catchup-feed -d catchup-feed \
     --clean --if-exists < /tmp/pre-rename.dump
   ```

旧 `pulse_db-data` ボリュームは 2 の起動後も残る(自動削除されない)。引き継ぎ確認が済んだら
`docker volume rm pulse_db-data` で回収してよい。

> 注: systemd unit 名は `pulse.service` のまま(改名しない)。unit は `docker compose -f
> compose.pi.yml up -d` を呼ぶだけで、compose プロジェクト名とは独立。旧システムの
> `catchup-feed.service` と衝突させないため、あえて据え置いている(4章の注意も参照)。

## 4. systemd による常時稼働化

コンテナ自体は `restart: unless-stopped` で自己回復するが、**ブート時だけは順序が要る**: `TAILNET_IP` へのポートバインドは tailscaled が IP を持った後でないと失敗し、Docker の再起動ポリシーは「一度も起動に成功していないコンテナ」を再試行しない。そこで tailscaled 起動後に `up -d` を一度だけ実行する oneshot ユニットを入れる。

```bash
# WorkingDirectory を実パスに書き換えてから配置
sed "s|/home/CHANGEME/pulse|$HOME/catchup-feed|" deploy/systemd/pulse.service | \
  sudo tee /etc/systemd/system/pulse.service >/dev/null
sudo systemctl daemon-reload
sudo systemctl enable --now pulse.service
systemctl status pulse.service   # active (exited) なら正常
```

注意: 旧システムの `catchup-feed.service` とは**別 unit**。§9 の停止手順までは共存が正しい状態であり、旧側には触らない。

## 5. Cloudflare Tunnel — ルート追加【ユーザー作業】(U-9)

catchup-feed が公開するのは `radio.catchup-feed.com` → `127.0.0.1:8090`(公開リスナー)だけ。設定例と「公開してよいルート」の一覧は `deploy/cloudflared/config.example.yml` に記載。

1. DNS: 既存トンネルに向ける
   ```bash
   cloudflared tunnel route dns <既存トンネル名> radio.catchup-feed.com
   ```
   (またはCloudflare ダッシュボードの DNS / Zero Trust → Tunnels → Public Hostname で追加)
2. Ingress: config ファイル運用なら `/etc/cloudflared/config.yml` の ingress に
   `hostname: radio.catchup-feed.com → service: http://localhost:8090` を追記し、
   `sudo systemctl restart cloudflared`。ダッシュボード管理のトンネルなら Public Hostname 追加のみで完了。
3. 旧システムのルートは §9 の停止まで**そのまま残す**。
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

## トラブル時の見方(監視スタックは無い。これで足りる)

- コンテナ状態: `docker compose -f deploy/compose.pi.yml ps` / `docker logs catchup-feed-server|catchup-feed-worker`
- 要約フォールバックの発生: `summaries.provider` を見る(`docker exec -it catchup-feed-postgres psql -U catchup-feed -c "select provider, count(*) from summaries group by 1"`)
- 朝エピソードが無い日: 正常系の欠番(Mac 不在)か、radio の失敗通知(Discord/Slack の notify_error)かをまず確認
