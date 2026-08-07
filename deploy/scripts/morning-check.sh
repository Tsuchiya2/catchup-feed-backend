#!/usr/bin/env bash
# ============================================================
# morning-check.sh — 公開/私的フィードの外形チェック(Mac 側で実行、D-29)
# ============================================================
# 死活監視は「Mac 朝チェック方式」(D-29): 毎朝 radio バッチの後(launchd
# 05:45 = deploy/launchd/com.pulse.morningcheck.plist)に Mac から2系統を
# 叩き、異常時のみメールで知らせる。外部監視 SaaS は使わない(ゼロ円)。
#
#   1. 公開フィード(Cloudflare Tunnel 経由)
#      「Pi は生きているが配信だけ死んでいる」故障モード(2026-07-22
#      停電障害の 502)を検知する。200 / 401 を正常とみなす(401 は
#      フィードトークン無しアクセスへの正規応答で、このスクリプトに
#      トークンを埋め込まないために「配信面は生きている」証拠として使う)。
#   2. 私的フィード(tailnet 経由 :8081)
#      tailnet 経路の断を検知する(2026-08-07 障害: Pi のノードキー
#      180日失効で Mac→Pi 全断、公開面は生きていたため7日間沈黙)。
#      200 のみ正常(トークン不要の tailnet バインドなので 401 は異常)。
#
# 2系統は独立に判定する(公開 OK でも私的 NG ならアラート)。どちらも
# 一時的な揺らぎで誤報しないよう、既定 3 回(20 秒間隔)試して全滅の
# ときだけ異常と確定する。メール件名で公開/私的どちらの断かを区別する。
#
# メール送信:
#   ~/pulse/.env の SMTP_*(Pi 側 worker と同じキー体系)を読み、
#   alert-mail.sh(同じディレクトリ)経由で curl の SMTP 機能で送る。
#   SMTP_ENABLED=true でない・設定不足の間はログに ALERT を残して送信を
#   スキップするだけで、スクリプト自体は失敗させない(常に exit 0)。
#
# 使い方:
#   morning-check.sh [公開URL]  # 省略時は PULSE_FEED_CHECK_URL か既定値。
#                               # 引数を渡しても私的チェックは実行される
#   疑似異常テスト: morning-check.sh https://nonexistent.invalid/
#
# 環境変数(~/pulse/.env、deploy/env.mac.example 参照):
#   PULSE_FEED_CHECK_URL          公開チェック先(既定: https://radio.catchup-feed.com/)
#   PULSE_PRIVATE_FEED_CHECK_URL  私的チェック先
#                                 (既定: http://ubuntu.tailf91c78.ts.net:8081/private/feed.xml)
#   PULSE_FEED_CHECK_ATTEMPTS     試行回数(既定: 3。両系統共通)
#   PULSE_FEED_CHECK_WAIT         試行間隔秒(既定: 20。両系統共通)
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

# SMTP 直送ヘルパー(同じディレクトリに配置。mac.md 10b章)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SCRIPT_DIR/alert-mail.sh" ]; then
    # shellcheck source=deploy/scripts/alert-mail.sh
    . "$SCRIPT_DIR/alert-mail.sh"
else
    log "WARN: $SCRIPT_DIR/alert-mail.sh がない(mac.md 10b章)。メール送信は無効"
    send_alert_mail() { log "alert-mail.sh 不在のため送信スキップ: ${1:-}"; cat >/dev/null; return 0; }
fi

PUBLIC_URL="${1:-${PULSE_FEED_CHECK_URL:-https://radio.catchup-feed.com/}}"
PRIVATE_URL="${PULSE_PRIVATE_FEED_CHECK_URL:-http://ubuntu.tailf91c78.ts.net:8081/private/feed.xml}"
ATTEMPTS="${PULSE_FEED_CHECK_ATTEMPTS:-3}"
WAIT="${PULSE_FEED_CHECK_WAIT:-20}"

# check_feed <label> <url> <正常コード(スペース区切り)>
# 成功で 0。全滅なら最後の HTTP コードを last_code に入れて 1 を返す
last_code="000"
check_feed() {
    _label="$1"
    _url="$2"
    _ok_codes="$3"
    _code="000"
    for i in $(seq 1 "$ATTEMPTS"); do
        # 失敗時も止まらないように || true(000 のまま)。--max-time で確実に返す
        _code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 "$_url" || true)"
        for _ok in $_ok_codes; do
            if [ "$_code" = "$_ok" ]; then
                log "OK: [$_label] $_url -> $_code (attempt $i/$ATTEMPTS)"
                return 0
            fi
        done
        log "attempt $i/$ATTEMPTS: [$_label] $_url -> $_code"
        if [ "$i" -lt "$ATTEMPTS" ]; then
            sleep "$WAIT"
        fi
    done
    last_code="$_code"
    return 1
}

fail=0

# --- 1. 公開フィード(200/401 を正常とみなす) ---
if ! check_feed public "$PUBLIC_URL" "200 401"; then
    fail=1
    log "ALERT: public feed check FAILED: $PUBLIC_URL -> $last_code ($ATTEMPTS attempts)"
    send_alert_mail "[pulse] public feed check FAILED ($last_code)" <<EOF || true
morning-check detected a problem with the PUBLIC feed (Cloudflare Tunnel 経由).

  URL:      $PUBLIC_URL
  result:   HTTP $last_code (000 = connection failure/timeout)
  attempts: $ATTEMPTS
  host:     $(hostname) (Mac morning check, D-29)

Check on the Pi:
  ssh <pi> 'docker ps; docker logs --tail 50 catchup-feed-server'
  ssh <pi> 'sudo systemctl status pulse.service'
EOF
fi

# --- 2. 私的フィード(tailnet 経由。200 のみ正常) ---
if ! check_feed private "$PRIVATE_URL" "200"; then
    fail=1
    log "ALERT: private feed check FAILED: $PRIVATE_URL -> $last_code ($ATTEMPTS attempts)"
    pi_host="${PRIVATE_URL#*://}"
    pi_host="${pi_host%%[:/]*}"
    send_alert_mail "[pulse] private feed check FAILED ($last_code)" <<EOF || true
morning-check detected a problem with the PRIVATE feed (tailnet 経由 :8081).

  URL:      $PRIVATE_URL
  result:   HTTP $last_code (000 = connection failure/timeout)
  attempts: $ATTEMPTS
  host:     $(hostname) (Mac morning check, D-29)

公開フィードが OK でこれだけ NG なら、tailnet 経路(Tailscale)の断が濃厚。
2026-08-07 障害は Pi のノードキー 180 日失効が原因だった。

Check from the Mac:
  tailscale status                  # Pi(ubuntu)が offline / key expired になっていないか
  ping -c 3 $pi_host
Check on the Pi(tailnet 断でも LAN / 物理コンソールから入れる):
  sudo tailscale status             # "Log in" 表示ならキー失効
  sudo tailscale up                 # 再認証(ブラウザで承認)
  # 恒久対策: Tailscale 管理画面で Pi ノードの key expiry を無効化する
EOF
fi

if [ "$fail" -eq 0 ]; then
    log "morning check finished: public/private とも OK"
fi
exit 0
