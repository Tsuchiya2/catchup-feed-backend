#!/usr/bin/env bash
# ============================================================
# radio-run.sh — launchd から radio バッチを起動するラッパー(Mac)
# ============================================================
# やること:
#   1. ~/pulse/.env を読み込む(launchd は環境変数を持たないため)
#   2. tailnet プリフライト: DB ホストに到達できなければ tailscale up で
#      自己修復を試みる(2 秒間隔 × 30 回まで再チェック)。復旧しても
#      しなくても先へ進む
#   3. VOICEVOX Engine が起動していなければ起動し、応答を待つ
#   4. radio を実行(引数はそのまま透過: -dry-run 等)
#   5. 自分で起動した Engine だけ後始末する
#
# リトライはしない(§8: 失敗した日はエピソード欠番で正常。通知だけを
# 確実にする)。
#
# tailnet プリフライトは 2026-08-10 障害(Mac の不意の再起動後に Tailscale
# 未接続のまま → MagicDNS 名前解決失敗で人手介入まで毎朝 exit 1)の恒久
# 対策。「壊れても翌日勝手に戻る」(§8)ための自己修復で、radio 自体の
# リトライはしない。
#
# 失敗通知は2経路:
#   a. radio 自身が DB の jobs テーブルに notify_error を積み、Pi の
#      worker がメール送信(§8。DB が生きている場合)
#   b. radio が非ゼロ終了したら、このラッパーが Mac から SMTP 直送
#      (alert-mail.sh)。2026-08-07 障害(tailnet 断で DB に届かず
#      notify_error を積めない → 7日間沈黙)の恒久対策。
#      送信失敗・SMTP 未設定でも exit code は radio のまま変えない。
#
# 導入: deploy/mac.md 6章。alert-mail.sh と一緒に ~/pulse/bin/ に置いて
# chmod +x する。
# ============================================================
set -euo pipefail

PULSE_HOME="${PULSE_HOME:-$HOME/pulse}"
ENV_FILE="$PULSE_HOME/.env"
RADIO_BIN="$PULSE_HOME/bin/radio"

# launchd の PATH は最小限なので Homebrew(ffmpeg / rsync)を足す
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

log() { printf '%s radio-run: %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*" >&2; }

mkdir -p "$PULSE_HOME/logs"

# SMTP 直送ヘルパー(同じディレクトリに配置)。見つからない日は通知なしで
# radio を優先する(欠番よりはマシ)— WARN を残して no-op に落とす
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SCRIPT_DIR/alert-mail.sh" ]; then
    # shellcheck source=deploy/scripts/alert-mail.sh
    . "$SCRIPT_DIR/alert-mail.sh"
else
    log "WARN: $SCRIPT_DIR/alert-mail.sh がない(mac.md 6章)。失敗時の SMTP 直送は無効"
    send_alert_mail() { log "alert-mail.sh 不在のため送信スキップ: ${1:-}"; cat >/dev/null; return 0; }
fi

if [ ! -f "$ENV_FILE" ]; then
    log "ERROR: $ENV_FILE がない。deploy/env.mac.example から作成する"
    exit 1
fi
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

if [ ! -x "$RADIO_BIN" ]; then
    log "ERROR: $RADIO_BIN がない。mac.md 5章の手順でビルドする"
    exit 1
fi

# --- tailnet プリフライト(2026-08-10 障害の恒久対策) -----------------
# DATABASE_URL のホスト(tailnet MagicDNS)に届かなければ tailscale up で
# 自己修復を試みる。復旧しなくても radio へ進む(失敗すれば従来どおり
# 非ゼロ終了 → SMTP 直送。§8: radio 自体のリトライはしない)
# 接続先は DATABASE_URL からのみ導出する(実ホスト名をこの資材に持たない)。
# 対応するのは URL 形式のみ。未設定や keyword DSN(host=... port=...)では
# 抽出できないので、その日はプリフライトを WARN 付きでスキップする
TAILNET_HOST=""
TAILNET_PORT=""
_hp="$(printf '%s' "${DATABASE_URL:-}" | sed -nE 's#^[^:/]+://([^@/]*@)?([^/?]+).*$#\2#p')"
if [ -n "$_hp" ]; then
    TAILNET_HOST="${_hp%%:*}"
    case "$_hp" in
        *:*) TAILNET_PORT="${_hp##*:}" ;;
        # URL 形式でポート省略なら libpq の実際の接続先(標準 5432)に合わせる
        *) TAILNET_PORT="5432" ;;
    esac
fi
unset _hp

tailnet_reachable() {
    # DNS 解決(radio と同じくシステムリゾルバ経由)か TCP 到達のどちらか
    # が通れば OK
    dscacheutil -q host -a name "$TAILNET_HOST" 2>/dev/null | grep -q '^ip_address:' && return 0
    nc -z -G 3 "$TAILNET_HOST" "$TAILNET_PORT" >/dev/null 2>&1
}

if [ -z "$TAILNET_HOST" ]; then
    # 導出できない日はプリフライト(自己修復)だけを諦めて radio へ進む。
    # ここで exit すると下の「非ゼロ終了 → SMTP 直送」ハンドラまで到達せず
    # 通知が無音になる(2026-08-07 障害と同じ失敗モード)ため、落とさない。
    # tailnet が実際に切れていれば radio が非ゼロ終了して直送アラートが飛び、
    # Mac 朝チェック(morning-check.sh)も独立に私的フィード断を検知する
    log "WARN: tailnet preflight: DATABASE_URL からホストを導出できない(未設定か keyword DSN)。プリフライトをスキップして radio へ進む"
elif tailnet_reachable; then
    log "tailnet preflight: $TAILNET_HOST reachable"
else
    log "tailnet preflight: unreachable, attempting tailscale up"
    # GUI アプリが落ちていると CLI だけでは繋がらないことがあるので先に起こす
    open -ga Tailscale 2>/dev/null || true
    TS_CLI="/Applications/Tailscale.app/Contents/MacOS/Tailscale"
    if [ ! -x "$TS_CLI" ]; then
        TS_CLI="$(command -v tailscale || true)"
    fi
    if [ -n "$TS_CLI" ]; then
        # 認証切れ(ログアウト・キー失効)だと up は認証 URL を出して完了まで
        # ブロックする。無人実行で無期限ハングしないよう有界化する
        "$TS_CLI" up --timeout=30s >/dev/null 2>&1 || true
    else
        log "WARN: tailnet preflight: tailscale CLI が見つからない(open -ga のみで待つ)"
    fi
    # 2 秒間隔 × 30 回まで到達性を再チェック(nc の接続待ち込みで最長 2 分半程度)
    recovered=0
    for _ in $(seq 1 30); do
        if tailnet_reachable; then
            recovered=1
            break
        fi
        sleep 2
    done
    if [ "$recovered" -eq 1 ]; then
        log "tailnet preflight: recovered, $TAILNET_HOST reachable"
    else
        log "WARN: tailnet preflight: still unreachable — radio へ進む(失敗時は既存の通知経路)"
    fi
fi
# -----------------------------------------------------------------------

VOICEVOX_URL="${VOICEVOX_URL:-http://127.0.0.1:50021}"
ENGINE_PID=""

voicevox_up() {
    curl -sf --max-time 3 "$VOICEVOX_URL/version" >/dev/null 2>&1
}

# 直後の `trap cleanup EXIT` から呼ばれる。shellcheck は trap 経由の呼び出しを
# 追えず「未使用関数」と誤判定するため、この関数に限り抑制する。
# shellcheck disable=SC2329
cleanup() {
    if [ -n "$ENGINE_PID" ]; then
        log "stopping VOICEVOX Engine (pid $ENGINE_PID)"
        kill "$ENGINE_PID" 2>/dev/null || true
    fi
}
trap cleanup EXIT

if ! voicevox_up; then
    if [ -n "${VOICEVOX_ENGINE_DIR:-}" ] && [ -x "$VOICEVOX_ENGINE_DIR/run" ]; then
        log "starting VOICEVOX Engine from $VOICEVOX_ENGINE_DIR"
        "$VOICEVOX_ENGINE_DIR/run" --host 127.0.0.1 --port "${VOICEVOX_URL##*:}" \
            >>"$PULSE_HOME/logs/voicevox.log" 2>&1 &
        ENGINE_PID=$!
        # 初回起動はモデル読み込みで時間がかかる。最大 180 秒待つ
        for _ in $(seq 1 90); do
            voicevox_up && break
            sleep 2
        done
    fi
fi

if ! voicevox_up; then
    # VOICEVOX 不在のまま radio を回しても TTS 段で落ちるだけ(§8:
    # VOICEVOX 障害→当日スキップ)。radio に進ませて notify_error を積ませる
    log "WARN: VOICEVOX Engine not responding at $VOICEVOX_URL — radio 側の失敗通知に任せる"
fi

log "starting radio $*"
rc=0
"$RADIO_BIN" "$@" || rc=$?
log "radio exited with code $rc"

if [ "$rc" -ne 0 ]; then
    # DB 断などで radio が notify_error ジョブを積めなくても届くよう、
    # その場で SMTP 直送(2026-08-07 障害の恒久対策)。リトライはしない。
    err_log="$PULSE_HOME/logs/radio.err.log"
    err_tail="(no $err_log)"
    if [ -f "$err_log" ]; then
        err_tail="$(tail -n 20 "$err_log" 2>/dev/null || true)"
    fi
    send_alert_mail "[pulse] radio FAILED (exit $rc)" <<EOF || true
radio batch failed on the Mac.

  exit code: $rc
  time:      $(date '+%Y-%m-%dT%H:%M:%S%z')
  host:      $(hostname)

--- tail -n 20 $err_log ---
$err_tail

Check from the Mac:
  tailscale status                    # Pi が offline / key expired になっていないか
  psql "\$DATABASE_URL" -c 'select 1' # DB(tailnet 越し :5433)到達性

Check on the Pi:
  ssh <pi> 'docker ps; docker logs --tail 50 catchup-feed-worker'
  ssh <pi> 'sudo tailscale status'    # キー失効なら sudo tailscale up で再認証

失敗した日はエピソード欠番で正常(§8)。翌朝も失敗が続く場合のみ対処する。
EOF
fi
exit "$rc"
