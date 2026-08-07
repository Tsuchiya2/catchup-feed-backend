#!/usr/bin/env bash
# ============================================================
# alert-mail.sh — SMTP 直送ヘルパー(source して使う。単体実行しない)
# ============================================================
# radio-run.sh / morning-check.sh が共用するアラートメール送信。
# Pi の worker 経由(jobs テーブルの notify_error)ではなく、Mac から
# curl の SMTP 機能で直送する(追加依存なし)。
#
# 2026-08-07 障害の教訓: tailnet 断で DB に届かないとき、radio は
# notify_error ジョブ自体を積めず通知が7日間沈黙した。Mac 単独で
# 完結する通知経路をこのヘルパーに一本化する。
#
# 使い方(呼び出し元と同じディレクトリに置く):
#   . "$(dirname "$0")/alert-mail.sh"
#   send_alert_mail "[pulse] 件名" <<EOF
#   本文
#   EOF
#
# 前提: SMTP_* / NOTIFY_ERROR_EMAIL_TO が環境変数に読み込み済み
# (~/pulse/.env。キー体系は Pi 側 worker と共通。env.mac.example 参照)。
# 挙動: SMTP_ENABLED != true・設定不足なら送信スキップをログに残すだけ。
# 送信失敗を含め **常に return 0**(通知は best-effort。呼び出し元の
# exit code を変えない)。trap は張らない(呼び出し元の EXIT trap を
# 壊さないため。一時ファイルは関数内で後始末する)。
# ============================================================

# 呼び出し元が log() を定義していない場合の予備
if ! declare -F log >/dev/null 2>&1; then
    log() { printf '%s alert-mail: %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*" >&2; }
fi

# send_alert_mail <subject>   本文は stdin(heredoc)で渡す
send_alert_mail() {
    _subject="$1"

    if [ "${SMTP_ENABLED:-false}" != "true" ]; then
        log "SMTP_ENABLED != true のためメール送信をスキップ: $_subject"
        cat >/dev/null # stdin(本文)を読み捨てる
        return 0
    fi
    if [ -z "${SMTP_HOST:-}" ] || [ -z "${SMTP_USERNAME:-}" ] \
        || [ -z "${SMTP_PASSWORD:-}" ] || [ -z "${NOTIFY_ERROR_EMAIL_TO:-}" ]; then
        log "SMTP_HOST/SMTP_USERNAME/SMTP_PASSWORD/NOTIFY_ERROR_EMAIL_TO のいずれか未設定のため送信スキップ: $_subject"
        cat >/dev/null
        return 0
    fi

    _from="${SMTP_FROM:-$SMTP_USERNAME}"
    _port="${SMTP_PORT:-587}"
    # 465 は implicit TLS(smtps)、それ以外(587 等)は STARTTLS
    if [ "$_port" = "465" ]; then
        _smtp_url="smtps://${SMTP_HOST}:${_port}"
        _tls_opt=""
    else
        _smtp_url="smtp://${SMTP_HOST}:${_port}"
        _tls_opt="--ssl-reqd"
    fi

    _msg_file="$(mktemp)"
    {
        printf 'From: pulse alert <%s>\n' "$_from"
        printf 'To: %s\n' "$NOTIFY_ERROR_EMAIL_TO"
        printf 'Subject: %s\n' "$_subject"
        printf 'Date: %s\n' "$(date -R 2>/dev/null || date '+%a, %d %b %Y %H:%M:%S %z')"
        printf '\n'
        cat # stdin = 本文
    } >"$_msg_file"

    # shellcheck disable=SC2086
    if curl -sS $_tls_opt --url "$_smtp_url" \
        --user "${SMTP_USERNAME}:${SMTP_PASSWORD}" \
        --mail-from "$_from" --mail-rcpt "$NOTIFY_ERROR_EMAIL_TO" \
        --max-time 60 -T "$_msg_file"; then
        log "alert mail sent to $NOTIFY_ERROR_EMAIL_TO: $_subject"
    else
        log "ERROR: alert mail send failed(SMTP 設定・ネットワークを確認): $_subject"
    fi
    rm -f "$_msg_file"
    return 0
}
