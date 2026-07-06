#!/usr/bin/env bash
# ============================================================
# transcribe-run.sh — launchd から transcribe worker を起動するラッパー(Mac)
# ============================================================
# やること:
#   1. 夜間窓ガード: 窓外(02:30〜04:20 の外)で発火したら何もせず正常終了。
#      スリープ持ち越しで昼間にまとめて発火した場合に、worker の deadline が
#      「翌朝 04:15」に解決されて丸1日走るのを防ぐ(ジョブは翌夜に持ち越し)
#   2. ブリッジを EXIT trap で先に張る: radio(04:30)発火まで Mac を起こして
#      おく caffeinate。.env 不在・uv 不在などどの経路で死んでもブリッジは
#      実行される(Phase 2 側の故障で Phase 1 の radio を潰さない)
#   3. ~/pulse/.env を読み込む(launchd は環境変数を持たないため。
#      DATABASE_URL は radio と共用)
#   4. catchup-feed-ai の checkout(PULSE_AI_DIR)で uv run pulse-transcribe
#      を実行(引数はそのまま透過: --deadline 等。既定 deadline は 04:15)。
#      caffeinate -i -s で実行中のスリープを抑止する(AC 電源接続が前提)
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

# --- 1. 夜間窓ガード ---
# Mac がスリープ中に 03:00 を跨ぐと launchd は蓋開け時にまとめて発火する。
# その時点で worker を起こすと --deadline(既定 04:15)が「翌朝の 04:15」に
# 解決され、caffeinate と合わせて最長丸1日 Mac が寝られなくなる。窓外の発火は
# 「その夜は無かった」ことにする(§5.3 の持ち越しと同じ意味論。exit 0 が正常)。
# 手動実行(引数あり、または端末から)はガードの対象外。
NIGHT_WINDOW_START="02:30"
NIGHT_WINDOW_END="04:20"
now_hm=$(date +%H:%M)
if [ "$#" -eq 0 ] && [ ! -t 0 ]; then
    if [[ "$now_hm" < "$NIGHT_WINDOW_START" || "$now_hm" > "$NIGHT_WINDOW_END" ]]; then
        log "outside the night window ($NIGHT_WINDOW_START-$NIGHT_WINDOW_END, now $now_hm): skipping, jobs carry over (§5.3)"
        exit 0
    fi
fi

# --- 2. ブリッジ(EXIT trap で全経路を担保) ---
# pmset repeat はウェイクを1本しか持てず、Phase 2 では 02:55 ウェイクに
# 置き換えるため(ai.md 6章)、04:25 ウェイクの代わりがこのブリッジ。
# 既定 04:32 まで Mac を起こしておく(それを過ぎていれば何もしない)。
# TRANSCRIBE_BRIDGE_UNTIL は trap 発火時に評価されるので、後で読み込む
# ~/pulse/.env の値も反映される。手動実行で不要なら Ctrl-C(worker →
# ブリッジの順に切れる)。
bridge_to_radio() {
    local bridge_until bridge_epoch now_epoch
    bridge_until="${TRANSCRIBE_BRIDGE_UNTIL:-04:32}"
    bridge_epoch=$(date -j -f '%Y-%m-%d %H:%M' "$(date '+%Y-%m-%d') $bridge_until" +%s 2>/dev/null || echo 0)
    if [ "$bridge_epoch" -eq 0 ]; then
        log "WARN: TRANSCRIBE_BRIDGE_UNTIL='$bridge_until' を HH:MM として解釈できない。ブリッジをスキップ"
        return 0
    fi
    now_epoch=$(date +%s)
    if [ "$now_epoch" -lt "$bridge_epoch" ]; then
        log "holding wake assertion until $bridge_until (bridge to radio 04:30)"
        caffeinate -i -s -t $((bridge_epoch - now_epoch)) || true
    fi
}
trap bridge_to_radio EXIT

# --- 3. 環境と前提の検証(ここから先はどこで死んでもブリッジが残る) ---
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

# --- 4. 実行 ---
# ~/pulse/.env で export された値が pydantic-settings の最優先になる
# (リポジトリ直下に開発用 .env があっても環境変数が勝つ)。
# caffeinate -i -s: 文字起こし中(最長 04:15 まで)のスリープを抑止。
# AC 電源接続が前提(ai.md 6章)。バッテリー駆動の夜は抑止できず途中で
# 寝ることがあるが、ジョブは翌夜に持ち越されるだけ(縮退許容)。
cd "$PULSE_AI_DIR"
log "starting pulse-transcribe $*"
rc=0
caffeinate -i -s uv run pulse-transcribe "$@" || rc=$?
log "pulse-transcribe exited with code $rc"
exit "$rc"
