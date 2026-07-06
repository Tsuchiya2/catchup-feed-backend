#!/usr/bin/env bash
# ============================================================
# transcribe-run.sh — launchd から transcribe worker を起動するラッパー(Mac)
# ============================================================
# やること:
#   1. ~/pulse/.env を読み込む(launchd は環境変数を持たないため。
#      DATABASE_URL は radio と共用)
#   2. catchup-feed-ai の checkout(PULSE_AI_DIR)で uv run pulse-transcribe
#      を実行(引数はそのまま透過: --deadline 等。既定 deadline は 04:15)。
#      caffeinate -i -s で実行中のスリープを抑止する(AC 電源時)
#   3. 終了後、radio(04:30)の発火時刻まで caffeinate でブリッジする。
#      pmset repeat はウェイクを1本しか持てず、Phase 2 では 02:55 ウェイク
#      に置き換えるため(ai.md 6章)、04:25 ウェイクの代わりがこのブリッジ
#
# リトライはしない(§5.3: 失敗・不在の夜は jobs に残って翌夜持ち越しが正常。
# attempts 上限 3 とリトライ間隔は worker 自身が jobs テーブルで管理する)。
#
# 導入: deploy/ai.md 2章・6章。~/pulse/bin/ に置いて chmod +x する。
# ============================================================
set -euo pipefail

PULSE_HOME="${PULSE_HOME:-$HOME/pulse}"
ENV_FILE="$PULSE_HOME/.env"

# launchd の PATH は最小限なので Homebrew(uv)を足す
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

log() { printf '%s transcribe-run: %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*" >&2; }

mkdir -p "$PULSE_HOME/logs"

if [ ! -f "$ENV_FILE" ]; then
    log "ERROR: $ENV_FILE がない。deploy/env.mac.example から作成する"
    exit 1
fi
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

# catchup-feed-ai の checkout 位置(ai.md 2章の規約)
PULSE_AI_DIR="${PULSE_AI_DIR:-$PULSE_HOME/catchup-feed-ai}"
if [ ! -f "$PULSE_AI_DIR/pyproject.toml" ]; then
    log "ERROR: $PULSE_AI_DIR に catchup-feed-ai がない。ai.md 2章の手順で clone する"
    exit 1
fi

if ! command -v uv >/dev/null 2>&1; then
    log "ERROR: uv がない。ai.md 3章の手順で導入する(brew install uv)"
    exit 1
fi

# 実行。~/pulse/.env で export された値が pydantic-settings の最優先になる
# (リポジトリ直下に開発用 .env があっても環境変数が勝つ)。
# caffeinate -i -s: 文字起こし中(最長 04:15 まで)のアイドルスリープを抑止。
# バッテリー駆動の夜は抑止できず途中で寝ることがあるが、ジョブは
# 翌夜スイープで回収されるだけ(縮退許容)。
cd "$PULSE_AI_DIR"
log "starting pulse-transcribe $*"
rc=0
caffeinate -i -s uv run pulse-transcribe "$@" || rc=$?
log "pulse-transcribe exited with code $rc"

# radio(04:30)発火までのブリッジ。worker は deadline(既定 04:15)か
# 予算消化で終わるため、放置すると 04:30 前に Mac が寝て radio が
# 発火しない。既定 04:32 まで起こしておく(それを過ぎていれば何もしない)。
# 深夜の手動実行でもこのブリッジが効く。不要なら Ctrl-C で切ってよい。
BRIDGE_UNTIL="${TRANSCRIBE_BRIDGE_UNTIL:-04:32}"
now_epoch=$(date +%s)
bridge_epoch=$(date -j -f '%Y-%m-%d %H:%M' "$(date '+%Y-%m-%d') $BRIDGE_UNTIL" +%s 2>/dev/null || echo 0)
if [ "$now_epoch" -lt "$bridge_epoch" ]; then
    log "holding wake assertion until $BRIDGE_UNTIL (bridge to radio 04:30)"
    caffeinate -i -s -t $((bridge_epoch - now_epoch)) || true
fi

exit "$rc"
