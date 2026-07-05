#!/usr/bin/env bash
# ============================================================
# radio-run.sh — launchd から radio バッチを起動するラッパー(Mac)
# ============================================================
# やること:
#   1. ~/pulse/.env を読み込む(launchd は環境変数を持たないため)
#   2. VOICEVOX Engine が起動していなければ起動し、応答を待つ
#   3. radio を実行(引数はそのまま透過: -dry-run 等)
#   4. 自分で起動した Engine だけ後始末する
#
# リトライはしない(§8: 失敗した日はエピソード欠番で正常。radio 自身が
# notify_error ジョブを積み、worker が通知する)。
#
# 導入: deploy/mac.md 6章。~/pulse/bin/ に置いて chmod +x する。
# ============================================================
set -euo pipefail

PULSE_HOME="${PULSE_HOME:-$HOME/pulse}"
ENV_FILE="$PULSE_HOME/.env"
RADIO_BIN="$PULSE_HOME/bin/radio"

# launchd の PATH は最小限なので Homebrew(ffmpeg / rsync)を足す
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

log() { printf '%s radio-run: %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*" >&2; }

mkdir -p "$PULSE_HOME/logs"

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

VOICEVOX_URL="${VOICEVOX_URL:-http://127.0.0.1:50021}"
ENGINE_PID=""

voicevox_up() {
    curl -sf --max-time 3 "$VOICEVOX_URL/version" >/dev/null 2>&1
}

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
exit "$rc"
