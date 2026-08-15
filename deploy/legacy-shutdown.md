# 旧 catchup-feed 停止手順(設計書 §9 の具体化)

**ステータス(2026-08-15 現在)**: 旧 catchup-feed の停止は **2026-07-06 に完了済み**。**1〜7章の手順はすべて完了し、未実施の手順はゼロ** — 1〜4・6〜7章は**実施済みの履歴**であり、そのまま再実行するものではない(章冒頭の注記を必ず読むこと。特に3章と6章は実行対象が存在しない)。**5章は「対象なし」で完了**(Archive すべき別リポジトリが存在しない。理由は5章)。最後まで残っていた4章の `catchup.catchup-feed.com` 削除も **2026-08-15 に完了**。

**手順とは別に、停止後の棚卸しで見つかった残骸の対応状況は8章のチェックリストが正**。そちらには**未対応が3件**残っている(ローカル ingress の陳腐化 / `~/crontab.bak-20260726` / `cloudflared-update.timer`。いずれも現状で実害は出ていない後片付け)。

実施条件(着手時の判断基準。2026-07-06 に達成済み): **pulse Phase 1 の完了条件達成** — 本人+友人1名がポッドキャストアプリで1週間購読できたこと(U-14 → U-15)。

方針(§9): 旧 DB から持ち越すのは sources 定義のみ。それ以外のデータ(記事・要約・通知履歴)は捨てる。旧リポジトリはアーカイブして残す(**この最後の1点は対象が存在せず不要だった。5章**)。

前提の確認(いずれも当時の状態): 旧スタックは Pi 上の旧リポジトリの compose(コンテナ名 `catchup-postgres` / `catchup-server` / `catchup-worker` に加え、**監視系の `catchup-prometheus` / `catchup-grafana`** の計5本。ポート 8080/5432/9091)で稼働していた。後継スタック(コンテナ名 `catchup-feed-postgres` / `catchup-feed-server` / `catchup-feed-worker`、8090/8081/5433)とは完全に分離されているため、以下の手順は後継に影響しない。**旧 `catchup-*` と新 `catchup-feed-*` はハイフンの後が違うだけで紛らわしい**ので、停止コマンドを打つ前に必ずコンテナ名を確認する(落とすのは `catchup-*` の方)。

---

## 1. sources 移植の確認(移植自体は済んでいる)→ **実施済み(2026-07-06)**

> **この章に実行対象はもう無い。** 比較元の初代 DB(`catchup-postgres`)が存在しないため、
> 以下の diff は再現できない。以下は履歴。

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

## 2. 旧 DB の最終スナップショット(保険。ゼロ円)→ **実施済み(2026-07-06)**

> **この章に実行対象はもう無い。** dump 元の `catchup-postgres` は存在しない。
> 取得済みのスナップショットは Mac 側(`~/pulse/backups/`)にあり、90 日で破棄してよい。以下は履歴。

データは持ち越さない方針だが、消す前に一度だけ dump を Mac のバックアップ置き場に退避しておく(後から「あの記事の要約を見たい」となったときの保険。90 日残して消してよい):

```bash
# Mac から実行
ssh <pi-user>@<pi の MagicDNS 名> \
  'docker exec catchup-postgres sh -c '\''pg_dumpall -U "$POSTGRES_USER"'\''' \
  | gzip > ~/pulse/backups/legacy-catchup-final-$(date +%Y%m%d).sql.gz
```

## 3. 旧コンテナの停止 → **完了(コンテナ停止 2026-07-06 / cron 2026-07-26 / systemd 2026-08-15)**

> **この章に実行対象はもう無い。** 初代のコンテナ・cron エントリ・systemd unit はいずれも残っていない
> (8章で実測確認)。**ここに並ぶ `docker stop catchup-*` や
> `sudo rm /etc/systemd/system/catchup-feed.service` を今そのまま打たないこと** — 対象は存在せず、
> 名前だけが pulse の現用資産と重なっている(8章「地雷」)。以下は履歴。

初代のチェックアウトは `/home/<pi-user>/catchup-feed` に置かれていた。これは**現在の pulse の親ディレクトリと同じパス**(8章「地雷」)なので、`cd` してから素の `docker compose down` を打つ形は使わない — カレントディレクトリの compose ファイルを拾って pulse を落とし得る。**プロジェクト名を明示**して落とす:

```bash
# Pi 上。まず落とす対象の実体を確認する(-a を付けないと停止中のものが出ない)
docker compose ls -a   # プロジェクト名。pulse は catchup-feed(現用。これは落とさない)
docker ps -a           # コンテナ名でも確認(初代 catchup-* / pulse catchup-feed-*)

cd ~                   # compose ファイルの無いディレクトリから(カレントの compose を拾わせない)
docker compose -p <初代のプロジェクト名> down   # ラベルベースで停止。ボリュームはまだ消さない(-v を付けない)
```

**`-p` で区別できないケースがある**。compose のプロジェクト名は明示 `name:` が無ければ**ディレクトリ名**が既定になり、初代のチェックアウトは `~/catchup-feed` にあった。つまり**初代のプロジェクト名も `catchup-feed` だった可能性が高く、それは現行 pulse の `compose.yml` の `name: catchup-feed` と同一**。`docker compose ls -a` の答えが `catchup-feed` しか無ければ**それは pulse なので `-p catchup-feed down` を打たない**。この場合はコンテナ名を直接指定して落とす:

```bash
# どちらの資産かをラベルで確認してから
docker inspect --format '{{.Name}} {{index .Config.Labels "com.docker.compose.project"}}' catchup-server

# 初代のコンテナ名(pulse は catchup-feed-*)。アプリ3本だけでなく監視系2本も動いていたので、
# 打つ対象は docker ps -a の一覧から拾って漏らさないこと
docker stop catchup-postgres catchup-server catchup-worker catchup-prometheus catchup-grafana
```

旧スタック用の crontab エントリ(backup.sh / health-check.sh / disk-usage-check.sh / docker-cleanup.sh 等)は **D-30(3) に沿って 2026-07-26 に削除済み**(バックアップが `~/crontab.bak-20260726` に残っている — 8章)。**残作業は無いが、念のため残存していないことだけ確認する**:

```bash
crontab -l          # 旧 catchup-feed 行が無いことを確認(通常は 0 件)
crontab -e          # 見つかった場合のみ、該当行を削除またはコメントアウト
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

## 4. Cloudflare Tunnel の旧ルート削除 → **完了(2026-08-15)**

> **この章に実行対象はもう無い。** ただし実際の経緯は、本章が想定していた「旧ルートだけを消す」形には
> ならなかった。時系列は次のとおり:
>
> - **2026-07-06** — 初代スタック停止。Cloudflare 側のホスト名はそのまま残った
> - **2026-07-11** — 旧ダッシュボード向けの `catchup.catchup-feed.com` が**初代ポート 8080 を向いたまま**で
>   管理 API が 502 になっていたため、Public Hostname を pulse の 8090 へ**向け直した**(削除ではない)。
>   **この日に削除されたルートは1本もない。** 以後 08-15 まで、pulse の公開リスナーは `radio.` と
>   `catchup.` の**2つの入口で露出**していた
> - **2026-08-15** — ユーザーが Cloudflare コンソールで `catchup.` の Public Hostname と DNS レコードを削除。
>   あわせて初代由来の `grafana` / `prometheus` も DNS にレコードが無いことを実測確認(8章)
>
> 以下は当時の手順と、削除前に行った参照元確認の記録。

1. `/etc/cloudflared/config.yml`(またはダッシュボードの Public Hostname)から**旧システム向けのルートだけ**を削除。`radio.catchup-feed.com`(pulse)と `pulse.catchup-feed.com`(ダッシュボード)は残す。
2. `sudo systemctl restart cloudflared`
3. Cloudflare DNS で旧ホスト名の CNAME レコードを削除。
4. 検証: 旧 URL が 404/解決不能になり、`radio.catchup-feed.com/feeds/<token>/feed.xml` は引き続き 200。

### `catchup.catchup-feed.com` の残置(2026-08-15 判明)→ **同日削除済み**

2026-07-06 の旧システム停止では旧ダッシュボード向けの `catchup.catchup-feed.com` が Cloudflare 側に残った。さらに 2026-07-11 の 502 復旧でこの Public Hostname を **pulse の 8090 へ向け直した**ため、08-15 までの間 **`catchup.` は pulse の公開リスナーに繋がった2つ目の公開入口**になっていた。「使っていないホスト名が残っていた」のではなく、**誰も参照していないのに生きた入口として露出していた**のが実態である。現用の Tunnel は remote managed(ダッシュボードの Public Hostname が正。pi.md 5章)で、Pi 上のファイルを見ても気づけないため見落とした。`AUTH_COOKIE_DOMAIN=.catchup-feed.com` はワイルドカードなので、**この入口にも管理ダッシュボードの認証クッキーが送信される**状態だった。

**2026-08-15 にユーザーが Cloudflare コンソールで Public Hostname と DNS レコードを削除した**。同日の外形確認で測ったのは次の3点(手順 4 のうち「旧 URL が解決不能」と「`radio.` の Tunnel ルートが無傷」まで):

| ホスト名 | 応答 | 判定 |
|---|---|---|
| `catchup.catchup-feed.com` | 解決不能(DNS レコードなし) | 削除完了 |
| `radio.catchup-feed.com`(`/`) | 401 | Tunnel ルートは無傷。401 は**ルート(`/`)がデフォルトの JWT 保護ハンドラに落ちた応答**で、「認証付きで公開されている」の意味ではない(pi.md 7章の注と同じ) |
| `pulse.catchup-feed.com` | 200 | ダッシュボードは無影響 |

**手順 4 の `radio.catchup-feed.com/feeds/<token>/feed.xml` → 200 は測っていない**(フィードトークンを手元に出さないため)。この軸は毎朝 05:45 の morning-check(mac.md 10b 章)が公開フィードの外形を叩いているので、**翌朝以降アラートが出なければ裏が取れる**。

以下は削除前に行った「本当に誰も参照していないか」の確認手順。**同種の判断(あるホスト名を消してよいか)が再び必要になったときの型として残す**。Vercel コンソールにログインしなくても、**外形のレスポンスヘッダと Pi の env の突き合わせだけで判定できる**:

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

3つとも `catchup.` を含まなければ参照元は無い。**削除の判断に用いたのは 1 と 2**(1 は `connect-src 'self' https://radio.catchup-feed.com`、2 は `https://radio.catchup-feed.com` = `radio.` 使用で確定)。3 は本書に後から足した軸で、削除時点では打っていない — 上の実測どおり削除後もダッシュボードは 200 で、結論は変わらなかった。

注: 2 は `compose.yml` に `${FEED_PUBLIC_BASE_URL:-https://radio.catchup-feed.com}` の既定があるため、**この出力は「`.env` で明示設定されている」ことの証明にはならない**(実効値であることは変わらないので判定の結論は同じ)。3 は compose 側が `:?`(未設定なら起動失敗)なので `.env` の実値がそのまま出る。

削除は remote managed の Tunnel に対して行ったため、`/etc/cloudflared/config.yml` の編集と cloudflared の再起動(手順 2)は不要だった。**ただし Pi のローカル ingress は陳腐化したまま残っている**(8章「未対応」)。

## 5. 旧リポジトリのアーカイブ → **対象なし(2026-08-15 確認)**

当初の想定(§9-4)は「GitHub で旧リポジトリを Archive し、設計学習の参照元として残す」だったが、**Archive すべき別リポジトリは存在しない**。初代 catchup-feed の実装は**本リポジトリそのもの**で、pulse へは新規リポジトリを起こさず作り替えた(PR #72「pulse Phase 1」、2026-07-05 マージ。253 ファイル・約9万行の削除を含む。`git diff --shortstat 54a818f^1 54a818f` で確認できる)。したがって初代のコードは**本リポジトリの git 履歴として残っており**、本リポジトリは現行なので Archive できない。

同名で始まる別リポジトリはいずれも **private で公開リスクが無く**、pulse の運用にも関与しないため放置してよい。

**この章に実施すべきユーザー作業は無い。** Pi 上のチェックアウトの扱いは6章。

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
docker compose ls -a   # 落とす対象のプロジェクト名(-a で停止中も出す)。pulse は catchup-feed(落とさない)
docker ps -a           # コンテナ名でも確認(初代 catchup-* / pulse catchup-feed-*)
```

確認したうえで、**プロジェクト名を明示して**落とす(`cd` + 素の `docker compose` はカレントディレクトリの compose ファイルを拾うため、pulse のディレクトリにいると pulse の DB ボリュームごと消える):

```bash
cd ~                                              # compose ファイルの無いディレクトリから
docker compose -p <初代のプロジェクト名> down -v   # 旧 DB 実体ごと削除
docker image prune -a                             # 使用中(pulse)のイメージは消えない
```

3章と同じく、**`docker compose ls -a` に `catchup-feed` しか出てこない場合それは pulse** なので `-p catchup-feed down -v` を打たない(pulse の DB ボリュームが消える)。初代だけを消すならコンテナ名・ボリューム名を直接指定し、`docker volume ls` の結果を目視してから落とす。

`docker compose down -v` の前に 2章の最終スナップショットが取れていることを必ず確認。旧リポジトリのチェックアウトを消すときは、`rm -rf` の引数が pulse の親ディレクトリでないことを**打つ前に目視する**。

## 7. 完了の記録

親セッションに報告し、進捗ログとロードマップの U-15 を完了にしてもらう。以降の定常運用はロードマップ「定常運用」の表(月次バックアップ確認・四半期リストア試験)に従う。いずれも親ディレクトリ側で管理しており、**本リポジトリには含まれない**。

## 8. 初代の残骸チェックリスト(2026-08-15 の棚卸し)

旧システムは 2026-07-06 に停止したが、その後の棚卸しで**コンテナ・cron・Cloudflare ルート以外の残骸**が見つかった。同種の停止作業をするとき、および「なぜか `degraded` / ディスクが減らない」ときはここを一巡する。

### 対応済み・不在を確認済み(2026-08-15)

- `/etc/systemd/system/catchup-feed.service` — 削除 + `daemon-reload` + `reset-failed`(3章)。systemd から catchup 系が消え、`systemctl --failed` に残るのは OS 由来のものだけになった
- `/etc/logrotate.d/catchup-cron` / `/etc/logrotate.d/catchup-email` — 削除。対象のログはもう生成されない。pulse で必要なのは `pulse-health-check` のみ(pi.md 9章)
- `/home/<pi-user>/backups/` の初代 DB ダンプ8本とログ類 — 削除(13MB → 4KB)。2章の最終スナップショットは Mac 側に退避済みで、Pi 側に保持する理由がない
- **初代の docker 資産(コンテナ・イメージ・ボリューム)とチェックアウト** — いずれも撤去済み(6章)。2026-08-15 に `docker ps -a` / `docker compose ls -a` / `docker volume ls` / `docker image ls` で確認し、残っているのは pulse の3コンテナとその資産だけだった。**このとき `docker compose ls -a` の答えは `catchup-feed` 1件のみ**で、これは pulse。3章の「答えが `catchup-feed` しか無ければ打つな」が現実に即していることの実測でもある
- **旧リポジトリの Archive(5章)** — 対象なし。初代の実装は本リポジトリの git 履歴そのものなので Archive すべき別リポジトリが存在しない(2026-08-15 確認)
- **Cloudflare の `catchup.catchup-feed.com`** — 2026-08-15 にユーザーがコンソールで Public Hostname と DNS レコードを削除(4章)。外形で `catchup.` が解決不能・`radio.` が 401・`pulse.` が 200 であることを同日確認済み
- **初代由来の `grafana` / `prometheus` の DNS CNAME** — 2026-08-15 に `dig +short grafana.catchup-feed.com` / `dig +short prometheus.catchup-feed.com` を実行し、**どちらもレコードが存在しない**ことを確認(同時に測った `catchup.` もレコードなし、`radio.` / `pulse.` はあり)。初代の ingress から外された際に DNS 側も片付いていたとみられる。`AUTH_COOKIE_DOMAIN` がワイルドカードである以上、これらの名前が再び生えれば同じく認証クッキーが飛ぶ点だけ覚えておく(**Pi のローカル `config.yml` には名前が残っている** — 下の「未対応」)
- **リポジトリ直下の初代由来ファイル 26 本(D-44)** — `scripts/`(health-check.sh / backup-db.sh 等)・初代期の tests / config / `internal/config` を削除(PR #116、2026-08-15)。Pi の現行運用が使っていたのは `deploy/scripts/` 側だけで、直下の `scripts/` は**流用元にすらしない**もの(残っていると誤って配置される事故要因だった)
- あわせて `docker builder prune` で build cache 4.1GB のうち 3.3GB を回収(初代とは無関係だが同時に実施。**ディスク使用率 43% → 32%**。pi.md 11章)

作業後の確認: `pulse.service` の `ActiveEnterTimestamp` が変わっていない(= pulse を再起動していない)、3コンテナ healthy、公開リスナーの無効トークン応答が 404。

### 未対応

- `/etc/cloudflared/config.yml` のローカル ingress が陳腐化 — 現用 Tunnel は remote managed なので実害は出ていないが、`radio.catchup-feed.com` のエントリが無く初代由来の `grafana` / `prometheus` が残っている。**config ファイル運用に戻すとフィード配信が 404 に落ちる**(pi.md 5章の注を参照)
- `~/crontab.bak-20260726` — 旧 cron のバックアップ。中身を確認して不要なら削除
- `cloudflared-update.timer` — disabled のまま残置。cloudflared 自体は現用なので unit ごと消さない。実害なし

### 地雷: `catchup-feed` という名前の重なり

旧 unit の `WorkingDirectory` は `/home/<pi-user>/catchup-feed` で、これは **pulse の親ディレクトリと同一**(pulse は `~/catchup-feed/catchup-feed-backend` にチェックアウトしている)。さらに pulse の compose プロジェクト名は `compose.yml` の `name: catchup-feed` で、旧 unit 名とも一致する。この状態で `~/catchup-feed` 直下に compose ファイルが置かれると、旧 unit の `ExecStop`(`docker compose down`)が**稼働中の pulse を落とし得た**。unit を削除したので解消済み。

問題は個別のファイルではなく、**unit 名・ディレクトリ名・compose プロジェクト名・コンテナ名がすべて `catchup-feed` 系で重なっている構造そのもの**である(初代 `catchup-*` と pulse `catchup-feed-*` はハイフンの後が違うだけ — 冒頭の「前提の確認」も同じ話)。名前だけでは取り違えを防げないので、**停止・削除系のコマンドは打つ前に `systemctl cat <unit>` / `docker compose ls -a` / `docker ps -a` / `docker inspect` のラベルで実体を確認する**。プロジェクト名は既定でディレクトリ名になるため、**`-p` を付けても初代と pulse を区別できない場合がある**(3章参照)。
