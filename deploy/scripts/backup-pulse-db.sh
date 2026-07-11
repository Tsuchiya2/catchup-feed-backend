#!/usr/bin/env bash
# ============================================================
# backup-pulse-db.sh — pulse DB バックアップ + episodes ミラー(Mac 側で実行)
# ============================================================
# 設計書 §8: 夜間 pg_dump を Mac へ(tailnet 経由)。
# Pi は常時稼働・Mac は夜間のみなので、「起きている側=Mac が pull する」
# 方が確実(launchd 05:15 = radio の後。deploy/launchd/com.pulse.backup.plist)。
#
# やること:
#   1. ssh 経由で catchup-feed-postgres コンテナ内の pg_dump(カスタム形式)を取得
#   2. PULSE_BACKUP_RETENTION_DAYS(既定 30 日)より古い dump を削除
#   3. Pi の episodes/ を rsync でミラー(--delete なし = Mac 側は蓄積)
#      ※ radio はテンポラリディレクトリで生成して転送後に消すため、
#        生成元は Mac に残らない。§8 の「実質二重化」はこのミラーが担う。
#      Pi 側は D-4 で 45 日より古い mp3 が消えるが、ミラーには残る。
#   4. Pi の books/ を rsync でミラー(D-25 (6): 書籍 PDF もバックアップ対象。
#      --delete なし = ダッシュボードで削除してもミラーには残る)
#
# 失敗してもリトライしない(縮退許容)。実在確認は月次の定常運用タスク。
#
# 必要な環境変数(~/pulse/.env、deploy/env.mac.example 参照):
#   PULSE_PI_SSH                 ssh 先(例: user@pi.tailnet-name.ts.net)
#   PULSE_PI_EPISODES_DIR        Pi ホスト側の episodes 絶対パス
#   PULSE_PI_BOOKS_DIR           Pi ホスト側の books 絶対パス(D-25)
#   PULSE_BACKUP_DIR             保存先(既定: ~/pulse/backups)
#   PULSE_BACKUP_RETENTION_DAYS  dump の保持日数(既定: 30)
# ============================================================
set -euo pipefail

PULSE_HOME="${PULSE_HOME:-$HOME/pulse}"
ENV_FILE="$PULSE_HOME/.env"

log() { printf '%s backup-pulse-db: %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*" >&2; }

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

if [ -f "$ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$ENV_FILE"
    set +a
fi

: "${PULSE_PI_SSH:?PULSE_PI_SSH を ~/pulse/.env に設定する}"
BACKUP_DIR="${PULSE_BACKUP_DIR:-$PULSE_HOME/backups}"
RETENTION_DAYS="${PULSE_BACKUP_RETENTION_DAYS:-30}"
SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=15)

mkdir -p "$BACKUP_DIR/db" "$BACKUP_DIR/episodes"

# --- 1. pg_dump(コンテナ内の pg_dump を使うのでバージョン不整合が起きない) ---
stamp="$(date +%Y%m%d-%H%M%S)"
dump_file="$BACKUP_DIR/db/pulse-$stamp.dump"
tmp_file="$dump_file.tmp"

log "dumping pulse database from $PULSE_PI_SSH"
if ssh "${SSH_OPTS[@]}" "$PULSE_PI_SSH" \
    'docker exec catchup-feed-postgres sh -c '\''pg_dump -U "$POSTGRES_USER" -Fc "$POSTGRES_DB"'\''' \
    >"$tmp_file"; then
    if [ ! -s "$tmp_file" ]; then
        rm -f "$tmp_file"
        log "ERROR: dump が空。Pi 側の catchup-feed-postgres を確認する"
        exit 1
    fi
    mv "$tmp_file" "$dump_file"
    log "dump saved: $dump_file ($(du -h "$dump_file" | cut -f1 | tr -d ' '))"
else
    rm -f "$tmp_file"
    log "ERROR: pg_dump failed(Pi 停止中/ssh 鍵未設定/コンテナ名変更を確認)"
    exit 1
fi

# --- 2. 保持ポリシー ---
deleted=$(find "$BACKUP_DIR/db" -name 'pulse-*.dump' -mtime +"$RETENTION_DAYS" -print -delete | wc -l | tr -d ' ')
log "retention: removed $deleted dump(s) older than $RETENTION_DAYS days"

# --- 3. episodes ミラー(失敗しても dump は成功扱い) ---
if [ -n "${PULSE_PI_EPISODES_DIR:-}" ]; then
    log "mirroring episodes from $PULSE_PI_SSH:$PULSE_PI_EPISODES_DIR"
    if rsync -a -e "ssh ${SSH_OPTS[*]}" \
        "$PULSE_PI_SSH:$PULSE_PI_EPISODES_DIR/" "$BACKUP_DIR/episodes/"; then
        log "episodes mirror OK ($(ls "$BACKUP_DIR/episodes" | wc -l | tr -d ' ') files)"
    else
        log "WARN: episodes mirror failed(dump は保存済みなので継続)"
    fi
else
    log "PULSE_PI_EPISODES_DIR 未設定のため episodes ミラーはスキップ"
fi

# --- 4. books ミラー(D-25 (6)。失敗しても dump は成功扱い) ---
if [ -n "${PULSE_PI_BOOKS_DIR:-}" ]; then
    mkdir -p "$BACKUP_DIR/books"
    log "mirroring books from $PULSE_PI_SSH:$PULSE_PI_BOOKS_DIR"
    if rsync -a -e "ssh ${SSH_OPTS[*]}" \
        "$PULSE_PI_SSH:$PULSE_PI_BOOKS_DIR/" "$BACKUP_DIR/books/"; then
        log "books mirror OK ($(ls "$BACKUP_DIR/books" | wc -l | tr -d ' ') files)"
    else
        log "WARN: books mirror failed(dump は保存済みなので継続)"
    fi
else
    log "PULSE_PI_BOOKS_DIR 未設定のため books ミラーはスキップ"
fi

log "backup finished"
