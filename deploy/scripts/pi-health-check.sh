#!/usr/bin/env bash
# pi-health-check.sh — Pi ローカル5分監視(D-30-2)
#
# cron(5分ごと)から実行し、pulse の3コンテナの docker health と
# server のローカル HTTP 応答をチェックする。
#
# 旧 health-check.sh のスパム問題の修正(D-30):メールは**状態遷移時のみ**。
#   正常→異常: 「障害検知」1通(ただし連続2回異常で確定。1回目は保留)
#   異常→正常: 「復旧」1通
#   連続異常中: 送らない
# 起動直後の誤検知対策: pulse.service 起動から GRACE_SECONDS 以内は異常判定を保留。
#
# 送信は msmtp(~/.msmtprc の account gmail をそのまま使用)。
# 配置: /home/ubuntu/bin/pi-health-check.sh(Pi の checkout 外)。正はリポジトリの
#       deploy/scripts/pi-health-check.sh。変更時は scp で再配置する(deploy/pi.md 参照)。
set -u

CONTAINERS=(catchup-feed-postgres catchup-feed-server catchup-feed-worker)
# server 公開リスナーのローカル応答確認。無効トークンなので 401/404 が返れば正常
# (2xx/4xx = server が応答している。000/5xx = 異常)
HTTP_URL="http://127.0.0.1:8090/feeds/deadbeef/feed.xml"
MAIL_TO="${PULSE_HC_MAIL_TO:-yuji2tsuchiya@gmail.com}"
STATE_FILE="${PULSE_HC_STATE_FILE:-/var/log/catchup/pi-health-check.state}"
LOG_FILE="${PULSE_HC_LOG_FILE:-/var/log/catchup/pi-health-check.log}"
# テスト時に "[TEST] " 等を入れて実障害と区別する
SUBJECT_PREFIX="${PULSE_HC_SUBJECT_PREFIX:-}"
GRACE_SECONDS=300

log() { printf '%s %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$1" >>"$LOG_FILE"; }

send_mail() { # $1=subject $2=body
  {
    printf 'To: %s\n' "$MAIL_TO"
    printf 'Subject: %s\n' "$1"
    printf 'Content-Type: text/plain; charset=UTF-8\n'
    printf '\n%s\n' "$2"
  } | /usr/bin/msmtp -a gmail "$MAIL_TO"
}

# --- チェック本体 ---
FAILURES=""
for c in "${CONTAINERS[@]}"; do
  h=$(docker inspect --format '{{.State.Health.Status}}' "$c" 2>/dev/null || echo "missing")
  [ "$h" = "healthy" ] || FAILURES="${FAILURES}${c}: ${h}"$'\n'
done
code=$(curl -s -o /dev/null -m 10 -w '%{http_code}' "$HTTP_URL" 2>/dev/null || echo 000)
case "$code" in
  2??|4??) : ;;
  *) FAILURES="${FAILURES}server HTTP (${HTTP_URL}): code=${code}"$'\n' ;;
esac

prev="OK"
[ -f "$STATE_FILE" ] && prev=$(<"$STATE_FILE")
docker_now=$(docker ps --format 'table {{.Names}}\t{{.Status}}' 2>&1)

# --- 正常時 ---
if [ -z "$FAILURES" ]; then
  if [ "$prev" = "ALERTED" ]; then
    send_mail "${SUBJECT_PREFIX}[pulse] 復旧: Pi ヘルスチェック正常" \
"Pi の5分監視が復旧を検知しました。対応は不要です。

docker ps:
${docker_now}

host: $(hostname) / $(date '+%Y-%m-%d %H:%M:%S %Z')" \
      && log "RECOVERED: mail sent" \
      || log "RECOVERED: msmtp failed (exit $?)"
  fi
  echo "OK" >"$STATE_FILE"
  exit 0
fi

# --- 異常あり: 起動直後は判定保留 ---
ts=$(systemctl show pulse.service -p ActiveEnterTimestamp --value 2>/dev/null || true)
if [ -n "$ts" ] && [ "$ts" != "n/a" ]; then
  start_epoch=$(date -d "$ts" +%s 2>/dev/null || echo 0)
  if [ "$start_epoch" -gt 0 ] && [ $(($(date +%s) - start_epoch)) -lt "$GRACE_SECONDS" ]; then
    log "failure observed within startup grace (${GRACE_SECONDS}s); deferred: $(echo "$FAILURES" | tr '\n' ' ')"
    exit 0
  fi
fi

# --- 状態遷移 ---
case "$prev" in
  OK)
    # 1回目は保留(連続2回で確定)。メールは送らない
    echo "FAIL_PENDING" >"$STATE_FILE"
    log "first failure (pending): $(echo "$FAILURES" | tr '\n' ' ')"
    ;;
  FAIL_PENDING)
    send_mail "${SUBJECT_PREFIX}[pulse] 障害検知: Pi ヘルスチェック異常" \
"Pi の5分監視が異常を検知しました(2回連続)。

異常内容:
${FAILURES}
docker ps:
${docker_now}

host: $(hostname) / $(date '+%Y-%m-%d %H:%M:%S %Z')

復旧すれば「復旧」メールが1通届きます。連続異常中の再送はありません。" \
      && log "ALERT: mail sent: $(echo "$FAILURES" | tr '\n' ' ')" \
      || log "ALERT: msmtp failed (exit $?)"
    echo "ALERTED" >"$STATE_FILE"
    ;;
  ALERTED)
    # 連続異常中は送らない(ログにも毎回は書かない)
    ;;
  *)
    echo "FAIL_PENDING" >"$STATE_FILE"
    log "unknown state '${prev}' reset to FAIL_PENDING"
    ;;
esac
exit 0
