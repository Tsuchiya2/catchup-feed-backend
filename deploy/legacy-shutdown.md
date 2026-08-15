# 旧 catchup-feed 停止手順(設計書 §9 の具体化)

**ステータス(2026-08-15 現在)**: 旧 catchup-feed の停止は **2026-07-06 に完了済み**。1〜3・5〜7章は**実施済みの履歴**であり、そのまま再実行するものではない(特に6章は実行対象が存在しない — 章冒頭の警告を必ず読むこと)。**残る未実施は4章の `catchup.catchup-feed.com` 削除だけ**【ユーザー作業】。停止後の棚卸しで見つかった残骸の対応状況は8章のチェックリストが正。

実施条件(着手時の判断基準。2026-07-06 に達成済み): **pulse Phase 1 の完了条件達成** — 本人+友人1名がポッドキャストアプリで1週間購読できたこと(U-14 → U-15)。

方針(§9): 旧 DB から持ち越すのは sources 定義のみ。それ以外のデータ(記事・要約・通知履歴)は捨てる。旧リポジトリはアーカイブして残す。

前提の確認: 旧スタックは Pi 上の旧リポジトリの compose(コンテナ名 `catchup-postgres` / `catchup-server` / `catchup-worker`、ポート 8080/5432/9091)で稼働している。後継スタック(コンテナ名 `catchup-feed-postgres` / `catchup-feed-server` / `catchup-feed-worker`、8090/8081/5433)とは完全に分離されているため、以下の手順は後継に影響しない。**旧 `catchup-*` と新 `catchup-feed-*` はハイフンの後が違うだけで紛らわしい**ので、停止コマンドを打つ前に必ずコンテナ名を確認する(落とすのは `catchup-*` の方)。

---

## 1. sources 移植の確認(移植自体は済んでいる)

sources 定義の移植先は `internal/infra/db/seeds/sources.sql` で、pulse server の起動時に自動投入済み(冪等)。ここでは**取りこぼしがないかの確認だけ**行う。

```bash
# 旧 DB のアクティブな sources 一覧
docker exec catchup-postgres psql -U catchup -d catchup -At \
  -c "SELECT name || ' | ' || feed_url FROM sources WHERE active ORDER BY name" > /tmp/old-sources.txt

# 新(catchup-feed)側の sources 一覧
docker exec catchup-feed-postgres psql -U catchup-feed -d catchup-feed -At \
  -c "SELECT name || ' | ' || feed_url FROM sources ORDER BY name" > /tmp/new-sources.txt

diff /tmp/old-sources.txt /tmp/new-sources.txt
```

- 差分のうち「旧にだけあるもの」で今後も購読したいソースは、**pulse ダッシュボードから追加**する(seeds を直接編集して再起動でもよいが、運用の本線はダッシュボード)。
- 既知の意図的な差分: Webflow / NextJS / Remix のスクレイパー依存ソースは落としてある(全行 inactive だったため。seeds/sources.sql 冒頭コメント参照)。

## 2. 旧 DB の最終スナップショット(保険。ゼロ円)

データは持ち越さない方針だが、消す前に一度だけ dump を Mac のバックアップ置き場に退避しておく(後から「あの記事の要約を見たい」となったときの保険。90 日残して消してよい):

```bash
# Mac から実行
ssh <pi-user>@<pi の MagicDNS 名> \
  'docker exec catchup-postgres sh -c '\''pg_dumpall -U "$POSTGRES_USER"'\''' \
  | gzip > ~/pulse/backups/legacy-catchup-final-$(date +%Y%m%d).sql.gz
```

## 3. 旧コンテナの停止

初代のチェックアウトは `/home/<pi-user>/catchup-feed` に置かれていた。これは**現在の pulse の親ディレクトリと同じパス**(8章「地雷」)なので、`cd` してから素の `docker compose down` を打つ形は使わない — カレントディレクトリの compose ファイルを拾って pulse を落とし得る。**プロジェクト名を明示**して落とす:

```bash
# Pi 上。まず落とす対象の実体を確認する
docker compose ls   # プロジェクト名。pulse は catchup-feed(現用。これは落とさない)
docker ps           # コンテナ名でも確認(初代 catchup-* / pulse catchup-feed-*)

cd ~                # compose ファイルの無いディレクトリから(カレントの compose を拾わせない)
docker compose -p <初代のプロジェクト名> down   # ラベルベースで停止。ボリュームはまだ消さない(-v を付けない)
```

旧スタック用の crontab エントリ(backup.sh / health-check.sh / disk-usage-check.sh / docker-cleanup.sh 等、`scripts/README.md` の推奨スケジュールで入れたもの)を無効化する:

```bash
crontab -l          # 旧 catchup-feed 行を確認
crontab -e          # 該当行を削除またはコメントアウト
```

旧スタック用の systemd unit(`catchup-feed.service`)も同時に始末する。**`systemctl disable` だけでは failed 状態が残る**ため、`reset-failed` まで打つこと:

```bash
# 1) 先に中身を見る。ExecStop と WorkingDirectory が pulse を巻き込まないことの確認
#    (旧 unit の WorkingDirectory は pulse の親ディレクトリと同一だった。8章「地雷」)
systemctl cat catchup-feed.service

# 2) 自動起動を止める。**--now は付けない** — 旧 unit の ExecStop は `docker compose down` で、
#    WorkingDirectory 次第で稼働中の pulse を落とし得る。旧スタックの停止はこの章の冒頭で
#    プロジェクト名を明示して済ませており、unit 経由で stop させる必要はない
sudo systemctl disable catchup-feed.service

# 3) failed 記録を消してから unit ファイルを削除する(逆順だとアンロード済みで
#    `Unit not loaded` を返す systemd 版がある。引数なしの systemctl reset-failed でもよい)
sudo systemctl reset-failed catchup-feed.service
sudo rm /etc/systemd/system/catchup-feed.service
sudo systemctl daemon-reload

systemctl --failed            # catchup 系が残っていないこと
systemctl is-system-running   # degraded なら --failed の unit 名まで見る(OS 由来の常連は pi.md 参照)
```

実例(2026-08-15 の棚卸しで判明): 旧 unit は D-28 (3) に従って 2026-07-26 に `disable` 済みだったが `reset-failed` を打っていなかったため、**2026-07-23 のブート時の起動失敗記録が 2026-08-15 まで3週間残置**し、`systemctl is-system-running` が `degraded` を返し続けていた。unit 自体は起動しないので実害はないが、この状態だと**本物の障害が `systemctl --failed` の一覧に埋もれる**。

停止後 pulse が無事なことを確認: リポジトリルートで `docker compose -f compose.yml -f deploy/compose.pi.yml --env-file deploy/.env ps`(3コンテナ healthy)、公開フィードがスマホから取得できること。

## 4. Cloudflare Tunnel の旧ルート削除【ユーザー作業】

1. `/etc/cloudflared/config.yml`(またはダッシュボードの Public Hostname)から**旧システム向けのルートだけ**を削除。`radio.catchup-feed.com`(pulse)と `pulse.catchup-feed.com`(ダッシュボード)は残す。
2. `sudo systemctl restart cloudflared`
3. Cloudflare DNS で旧ホスト名の CNAME レコードを削除。
4. 検証: 旧 URL が 404/解決不能になり、`radio.catchup-feed.com/feeds/<token>/feed.xml` は引き続き 200。

### `catchup.catchup-feed.com` の残置(2026-08-15 判明)

2026-07-06 の旧システム停止では旧ダッシュボード向けの `catchup.catchup-feed.com` だけが Cloudflare 側に残っていた。現用の Tunnel は remote managed(ダッシュボードの Public Hostname が正。pi.md 5章)で、Pi 上のファイルを見ても気づけないため見落とした。`AUTH_COOKIE_DOMAIN=.catchup-feed.com` はワイルドカードなので、**使っていないホスト名にも管理ダッシュボードの認証クッキーが送信される**状態になる。放置しない。

削除の前に「本当に誰も参照していないか」を確認する。Vercel コンソールにログインしなくても、**外形のレスポンスヘッダと Pi の env の突き合わせだけで判定できる**:

```bash
# 1) ダッシュボードが叩く API のホスト名。frontend の next.config.ts が
#    NEXT_PUBLIC_API_URL から CSP を生成しているため、本番の実値が
#    connect-src にそのまま出る
curl -sI https://pulse.catchup-feed.com/ | grep -i content-security-policy

# 2) フィードの絶対 URL 生成に使っている値(Pi 側)
docker exec catchup-feed-server printenv FEED_PUBLIC_BASE_URL

# 3) ブラウザ由来で許可しているオリジン(`catchup.` が居ないことの直接証拠)
docker exec catchup-feed-server printenv CORS_ALLOWED_ORIGINS
```

3つとも `catchup.` を含まなければ参照元は無い。**この確認は 2026-08-15 に実施済み**で、1 は `connect-src 'self' https://radio.catchup-feed.com`、2 は `https://radio.catchup-feed.com` だった(= `radio.` 使用で確定)。

注: 2 は `compose.yml` に `${FEED_PUBLIC_BASE_URL:-https://radio.catchup-feed.com}` の既定があるため、**この出力は「`.env` で明示設定されている」ことの証明にはならない**(実効値であることは変わらないので判定の結論は同じ)。3 は compose 側が `:?`(未設定なら起動失敗)なので `.env` の実値がそのまま出る。

残っているのは **Cloudflare ダッシュボードでの Public Hostname 削除(上の手順 1)と DNS レコード削除(手順 3)だけ**【ユーザー作業】。remote managed なので `/etc/cloudflared/config.yml` の編集と cloudflared の再起動(手順 2)は不要。削除後に手順 4 の検証を行う。

## 5. 旧リポジトリのアーカイブ【ユーザー作業】

GitHub で旧リポジトリを Archive(Settings → Archive this repository)。設計学習の参照元として残置(§9-4)。Pi 上のチェックアウトは 6章の掃除まで残してよい。

## 6. 後片付け(1〜2週間の安定稼働を見てから)

> **この章に実行対象はもう無い(2026-08-15 の棚卸しで確認)。以下は履歴。**
> 初代のコンテナ・イメージ・ボリュームは消滅済みで、Pi に残る docker 資産は pulse の3コンテナだけ。
> 初代のチェックアウトも撤去済み。
>
> **`rm -rf ~/catchup-feed` を打たないこと。** `~/catchup-feed` は現在 **pulse の親ディレクトリ**で、
> 配下に `catchup-feed-backend/`・`episodes/`(mp3 アーカイブ)・`books/`(書籍 PDF)・
> `catchup-feed-backend/deploy/.env`(`POSTGRES_PASSWORD` / `JWT_SECRET` / API キー)がある。
> **`.env` は `backup-pulse-db.sh` のミラー対象(db / episodes / books)に入っていないため復旧手段がない。**
> 初代のチェックアウトも**このパスに置かれていた**(8章「地雷」)ので、「旧リポジトリの実パスに
> 読み替える」ことはできない — 読み替え先が存在しない。

万一もう一度この掃除が必要になったら、コマンドを打つ前に対象の実体を確認する(8章「地雷」の手順):

```bash
docker compose ls   # 落とす対象のプロジェクト名。pulse は catchup-feed(現用。これは落とさない)
docker ps           # コンテナ名でも確認(初代 catchup-* / pulse catchup-feed-*)
```

確認したうえで、**プロジェクト名を明示して**落とす(`cd` + 素の `docker compose` はカレントディレクトリの compose ファイルを拾うため、pulse のディレクトリにいると pulse の DB ボリュームごと消える):

```bash
cd ~                                              # compose ファイルの無いディレクトリから
docker compose -p <初代のプロジェクト名> down -v   # 旧 DB 実体ごと削除
docker image prune -a                             # 使用中(pulse)のイメージは消えない
```

`docker compose down -v` の前に 2章の最終スナップショットが取れていることを必ず確認。旧リポジトリのチェックアウトを消すときは、`rm -rf` の引数が pulse の親ディレクトリでないことを**打つ前に目視する**。

## 7. 完了の記録

親セッションに報告し、`docs/progress.md` と setup-and-roadmap.md の U-15 を完了にしてもらう。以降の定常運用は setup-and-roadmap.md「定常運用」の表(月次バックアップ確認・四半期リストア試験)に従う。

## 8. 初代の残骸チェックリスト(2026-08-15 の棚卸し)

旧システムは 2026-07-06 に停止したが、その後の棚卸しで**コンテナ・cron・Cloudflare ルート以外の残骸**が見つかった。同種の停止作業をするとき、および「なぜか `degraded` / ディスクが減らない」ときはここを一巡する。

### 対応済み(2026-08-15)

- `/etc/systemd/system/catchup-feed.service` — 削除 + `daemon-reload` + `reset-failed`(3章)。systemd から catchup 系が消え、`systemctl --failed` に残るのは OS 由来のものだけになった
- `/etc/logrotate.d/catchup-cron` / `/etc/logrotate.d/catchup-email` — 削除。対象のログはもう生成されない。pulse で必要なのは `pulse-health-check` のみ(pi.md 9章)
- `/home/<pi-user>/backups/` の初代 DB ダンプ8本とログ類 — 削除(13MB → 4KB)。2章の最終スナップショットは Mac 側に退避済みで、Pi 側に保持する理由がない
- あわせて `docker builder prune` で build cache 4.1GB のうち 3.3GB を回収(初代とは無関係だが同時に実施。**ディスク使用率 43% → 32%**。pi.md 11章)

作業後の確認: `pulse.service` の `ActiveEnterTimestamp` が変わっていない(= pulse を再起動していない)、3コンテナ healthy、公開リスナーの無効トークン応答が 404。

### 未対応

- **Cloudflare の `catchup.catchup-feed.com`【ユーザー作業】** — 4章の確認は完了済み(`radio.` 使用で確定)。ダッシュボードでの Public Hostname と DNS レコードの削除だけが残っている。あわせて**初代由来の `grafana` / `prometheus` の DNS CNAME が残っていないかも一巡する**(ローカル ingress に名前が残っているもの。`AUTH_COOKIE_DOMAIN` がワイルドカードである以上、これらのホスト名にも同じく認証クッキーが飛ぶ)
- `/etc/cloudflared/config.yml` のローカル ingress が陳腐化 — 現用 Tunnel は remote managed なので実害は出ていないが、`radio.catchup-feed.com` のエントリが無く初代由来の `grafana` / `prometheus` が残っている。**config ファイル運用に戻すとフィード配信が 404 に落ちる**(pi.md 5章の注を参照)
- `~/crontab.bak-20260726` — 旧 cron のバックアップ。中身を確認して不要なら削除
- `cloudflared-update.timer` — disabled のまま残置。cloudflared 自体は現用なので unit ごと消さない。実害なし

### 地雷: `catchup-feed` という名前の重なり

旧 unit の `WorkingDirectory` は `/home/<pi-user>/catchup-feed` で、これは **pulse の親ディレクトリと同一**(pulse は `~/catchup-feed/catchup-feed-backend` にチェックアウトしている)。さらに pulse の compose プロジェクト名は `compose.yml` の `name: catchup-feed` で、旧 unit 名とも一致する。この状態で `~/catchup-feed` 直下に compose ファイルが置かれると、旧 unit の `ExecStop`(`docker compose down`)が**稼働中の pulse を落とし得た**。unit を削除したので解消済み。

問題は個別のファイルではなく、**unit 名・ディレクトリ名・compose プロジェクト名・コンテナ名がすべて `catchup-feed` 系で重なっている構造そのもの**である(初代 `catchup-*` と pulse `catchup-feed-*` はハイフンの後が違うだけ — 冒頭の「前提の確認」も同じ話)。名前だけでは取り違えを防げないので、**停止・削除系のコマンドは打つ前に `systemctl cat <unit>` / `docker compose ls` / `docker ps` で実体を確認する**。
