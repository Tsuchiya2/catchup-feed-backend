#!/usr/bin/env bash
# ============================================================
# morning-check.sh — 公開フィードの外形チェック(Mac 側で実行、D-29)
# ============================================================
# 死活監視は「Mac 朝チェック方式」(D-29): 毎朝 radio バッチの後(launchd
# 05:45 = deploy/launchd/com.pulse.morningcheck.plist)に Mac から公開
# フィード URL を叩き、異常時のみメールで知らせる。
# 「Pi は生きているが配信だけ死んでいる」故障モード(2026-07-22 停電障害で
# 実際に起きた 502)を最大1日遅れで検知する(縮退許容 §8 と整合)。
# 外部監視 SaaS は使わない(ゼロ円)。
#
# 判定:
#   - 200 / 401 → 正常。401 はフィードトークン無しアクセスへの正規応答で、
#     このスクリプトにトークンを埋め込まないために 401 を「配信面は生きて
#     いる」証拠として使う。
#   - それ以外(5xx・タイムアウト・接続失敗 = 000)→ 異常。
#     一時的な揺らぎで誤報しないよう、既定 3 回(20 秒間隔)試して全滅の
#     ときだけ異常と確定する。
#
# メール送信:
#   ~/pulse/.env の SMTP_*(Pi 側 worker と同じキー体系)を読み、curl の
#   SMTP 機能で NOTIFY_ERROR_EMAIL_TO へ送る(追加依存なし)。
#   SMTP_ENABLED=true でない・設定不足の間はログに ALERT を残して送信を
#   スキップするだけで、スクリプト自体は失敗させない。
#
# 使い方:
#   morning-check.sh [URL]     # URL 省略時は PULSE_FEED_CHECK_URL か既定値
#   疑似異常テスト: morning-check.sh https://nonexistent.invalid/
#
# 環境変数(~/pulse/.env、deploy/env.mac.example 参照):
#   PULSE_FEED_CHECK_URL          チェック先(既定: https://radio.catchup-feed.com/)
#   PULSE_FEED_CHECK_ATTEMPTS     試行回数(既定: 3)
#   PULSE_FEED_CHECK_WAIT         試行間隔秒(既定: 20)
#   SMTP_ENABLED / SMTP_HOST / SMTP_PORT / SMTP_USERNAME / SMTP_PASSWORD /
#   SMTP_FROM / NOTIFY_ERROR_EMAIL_TO
# ============================================================
set -euo pipefail

PULSE_HOME="${PULSE_HOME:-$HOME/pulse}"
ENV_FILE="$PULSE_HOME/.env"

log() { printf '%s morning-check: %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*" >&2; }

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

if [ -f "$ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$ENV_FILE"
    set +a
fi

URL="${1:-${PULSE_FEED_CHECK_URL:-https://radio.catchup-feed.com/}}"
ATTEMPTS="${PULSE_FEED_CHECK_ATTEMPTS:-3}"
WAIT="${PULSE_FEED_CHECK_WAIT:-20}"

# --- 1. 外形チェック(200/401 を正常とみなす) ---
code="000"
for i in $(seq 1 "$ATTEMPTS"); do
    # 失敗時も止まらないように || true(000 のまま)。--max-time で確実に返す
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 "$URL" || true)"
    case "$code" in
        200|401)
            log "OK: $URL -> $code (attempt $i/$ATTEMPTS)"
            exit 0
            ;;
    esac
    log "attempt $i/$ATTEMPTS: $URL -> $code"
    [ "$i" -lt "$ATTEMPTS" ] && sleep "$WAIT"
done

log "ALERT: feed check FAILED: $URL -> $code ($ATTEMPTS attempts)"

# --- 2. 異常確定 → メール送信(SMTP 未設定ならログに残してスキップ) ---
if [ "${SMTP_ENABLED:-false}" != "true" ]; then
    log "SMTP_ENABLED != true のためメール送信をスキップ(U-11 完了後に有効化)"
    exit 0
fi
if [ -z "${SMTP_HOST:-}" ] || [ -z "${SMTP_USERNAME:-}" ] \
    || [ -z "${SMTP_PASSWORD:-}" ] || [ -z "${NOTIFY_ERROR_EMAIL_TO:-}" ]; then
    log "SMTP_HOST/SMTP_USERNAME/SMTP_PASSWORD/NOTIFY_ERROR_EMAIL_TO のいずれか未設定のため送信スキップ"
    exit 0
fi

from="${SMTP_FROM:-$SMTP_USERNAME}"
port="${SMTP_PORT:-587}"
# 465 は implicit TLS(smtps)、それ以外(587 等)は STARTTLS
if [ "$port" = "465" ]; then
    smtp_url="smtps://${SMTP_HOST}:${port}"
    tls_opt=""
else
    smtp_url="smtp://${SMTP_HOST}:${port}"
    tls_opt="--ssl-reqd"
fi

msg_file="$(mktemp)"
trap 'rm -f "$msg_file"' EXIT
cat >"$msg_file" <<EOF
From: pulse morning-check <${from}>
To: ${NOTIFY_ERROR_EMAIL_TO}
Subject: [pulse] feed check FAILED (${code})
Date: $(date -R 2>/dev/null || date '+%a, %d %b %Y %H:%M:%S %z')

morning-check detected a problem with the public feed.

  URL:      ${URL}
  result:   HTTP ${code} (000 = connection failure/timeout)
  attempts: ${ATTEMPTS}
  host:     $(hostname) (Mac morning check, D-29)

Check on the Pi:
  ssh <pi> 'docker ps; docker logs --tail 50 catchup-feed-server'
  sudo systemctl status pulse.service
EOF

# shellcheck disable=SC2086
if curl -sS $tls_opt --url "$smtp_url" \
    --user "${SMTP_USERNAME}:${SMTP_PASSWORD}" \
    --mail-from "$from" --mail-rcpt "$NOTIFY_ERROR_EMAIL_TO" \
    --max-time 60 -T "$msg_file"; then
    log "alert mail sent to $NOTIFY_ERROR_EMAIL_TO"
else
    log "ERROR: alert mail send failed(SMTP 設定・ネットワークを確認)"
fi
exit 0
