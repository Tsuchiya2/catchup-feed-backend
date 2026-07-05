# 旧 catchup-feed 停止手順(設計書 §9 の具体化)

実施条件(親セッションが確認してから着手): **pulse Phase 1 の完了条件達成** — 本人+友人1名がポッドキャストアプリで1週間購読できたこと(U-14 → U-15)。

方針(§9): 旧 DB から持ち越すのは sources 定義のみ。それ以外のデータ(記事・要約・通知履歴)は捨てる。旧リポジトリはアーカイブして残す。

前提の確認: 旧スタックは Pi 上の旧リポジトリの compose(コンテナ名 `catchup-postgres` / `catchup-server` / `catchup-worker`、ポート 8080/5432/9091)で稼働している。pulse スタック(`pulse-*`、8090/8081/5433)とは完全に分離されているため、以下の手順は pulse に影響しない。

---

## 1. sources 移植の確認(移植自体は済んでいる)

sources 定義の移植先は `internal/infra/db/seeds/sources.sql` で、pulse server の起動時に自動投入済み(冪等)。ここでは**取りこぼしがないかの確認だけ**行う。

```bash
# 旧 DB のアクティブな sources 一覧
docker exec catchup-postgres psql -U catchup -d catchup -At \
  -c "SELECT name || ' | ' || feed_url FROM sources WHERE active ORDER BY name" > /tmp/old-sources.txt

# pulse 側の sources 一覧
docker exec pulse-postgres psql -U pulse -d pulse -At \
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

```bash
# Pi 上、旧リポジトリのディレクトリで
cd ~/catchup-feed   # 旧リポジトリの実パスに読み替え
docker compose down   # ボリュームはまだ消さない(-v を付けない)
```

旧スタック用の crontab エントリ(backup.sh / health-check.sh / disk-usage-check.sh / docker-cleanup.sh 等、`scripts/README.md` の推奨スケジュールで入れたもの)を無効化する:

```bash
crontab -l          # 旧 catchup-feed 行を確認
crontab -e          # 該当行を削除またはコメントアウト
```

停止後 pulse が無事なことを確認: `docker compose -f ~/pulse/catchup-feed-backend/deploy/compose.pi.yml ps`(3コンテナ healthy)、公開フィードがスマホから取得できること。

## 4. Cloudflare Tunnel の旧ルート削除【ユーザー作業】

1. `/etc/cloudflared/config.yml`(またはダッシュボードの Public Hostname)から**旧システム向けのルートだけ**を削除。`radio.catchup-feed.com`(pulse)と `pulse.catchup-feed.com`(ダッシュボード)は残す。
2. `sudo systemctl restart cloudflared`
3. Cloudflare DNS で旧ホスト名の CNAME レコードを削除。
4. 検証: 旧 URL が 404/解決不能になり、`radio.catchup-feed.com/feeds/<token>/feed.xml` は引き続き 200。

## 5. 旧リポジトリのアーカイブ【ユーザー作業】

GitHub で旧リポジトリを Archive(Settings → Archive this repository)。設計学習の参照元として残置(§9-4)。Pi 上のチェックアウトは 6章の掃除まで残してよい。

## 6. 後片付け(1〜2週間の安定稼働を見てから)

```bash
# 旧コンテナ・イメージ・ボリューム(旧 DB 実体)を削除
cd ~/catchup-feed
docker compose down -v
docker image prune -a   # 使用中(pulse)のイメージは消えない

# 旧リポジトリのチェックアウトを削除(GitHub にアーカイブ済みが前提)
cd ~ && rm -rf ~/catchup-feed
```

`docker compose down -v` の前に 2章の最終スナップショットが取れていることを必ず確認。

## 7. 完了の記録

親セッションに報告し、`docs/progress.md` と setup-and-roadmap.md の U-15 を完了にしてもらう。以降の定常運用は setup-and-roadmap.md「定常運用」の表(月次バックアップ確認・四半期リストア試験)に従う。
