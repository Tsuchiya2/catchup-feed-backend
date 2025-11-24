#!/bin/bash
# ============================================================
# PostgreSQL データベースバックアップスクリプト
# ============================================================
# 自動バックアップと世代管理を実現
#
# 使用方法:
#   ./scripts/backup-db.sh [OPTIONS]
#
# オプション:
#   --retention DAYS  保持日数（デフォルト: 7日）
#   --output DIR      出力ディレクトリ（デフォルト: ./backups）
#   --compress        gzip圧縮を有効化
# ============================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# カラー出力
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# デフォルト設定
RETENTION_DAYS=7
OUTPUT_DIR="$PROJECT_ROOT/backups"
COMPRESS=false

# 引数パース
while [[ $# -gt 0 ]]; do
    case $1 in
        --retention)
            RETENTION_DAYS="$2"
            shift 2
            ;;
        --output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --compress)
            COMPRESS=true
            shift
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

echo ""
echo "============================================================"
echo "  💾 PostgreSQL Database Backup"
echo "============================================================"
echo "  Retention: $RETENTION_DAYS days"
echo "  Output: $OUTPUT_DIR"
echo "  Compress: $COMPRESS"
echo "============================================================"
echo ""

# 出力ディレクトリ作成
mkdir -p "$OUTPUT_DIR"

# タイムスタンプ
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
BACKUP_FILE="catchup_backup_${TIMESTAMP}.sql"

if [ "$COMPRESS" = true ]; then
    BACKUP_FILE="${BACKUP_FILE}.gz"
fi

BACKUP_PATH="$OUTPUT_DIR/$BACKUP_FILE"

# Docker Compose が起動しているか確認
cd "$PROJECT_ROOT"
if ! docker compose ps postgres | grep -q "Up"; then
    log_error "PostgreSQL container is not running!"
    log_info "Start with: docker compose up -d postgres"
    exit 1
fi

# バックアップ実行
log_info "Starting backup..."
log_info "Backup file: $BACKUP_FILE"

if [ "$COMPRESS" = true ]; then
    # 圧縮バックアップ
    if docker compose exec -T postgres pg_dumpall -U catchup | gzip > "$BACKUP_PATH"; then
        log_success "Backup completed (compressed)"
    else
        log_error "Backup failed!"
        rm -f "$BACKUP_PATH"
        exit 1
    fi
else
    # 非圧縮バックアップ
    if docker compose exec -T postgres pg_dumpall -U catchup > "$BACKUP_PATH"; then
        log_success "Backup completed"
    else
        log_error "Backup failed!"
        rm -f "$BACKUP_PATH"
        exit 1
    fi
fi

# ファイルサイズ確認
BACKUP_SIZE=$(du -h "$BACKUP_PATH" | cut -f1)
log_info "Backup size: $BACKUP_SIZE"

# 古いバックアップの削除
log_info "Cleaning old backups (older than $RETENTION_DAYS days)..."
DELETED_COUNT=0
while IFS= read -r -d '' file; do
    rm -f "$file"
    ((DELETED_COUNT++))
    log_info "Deleted: $(basename "$file")"
done < <(find "$OUTPUT_DIR" -name "catchup_backup_*.sql*" -type f -mtime +$RETENTION_DAYS -print0)

if [ $DELETED_COUNT -eq 0 ]; then
    log_info "No old backups to delete"
else
    log_success "Deleted $DELETED_COUNT old backup(s)"
fi

# 現在のバックアップ一覧
echo ""
log_info "Current backups:"
ls -lh "$OUTPUT_DIR"/catchup_backup_*.sql* 2>/dev/null | awk '{print "  " $9 " (" $5 ")"}'

echo ""
log_success "Backup completed successfully!"
log_info "Backup location: $BACKUP_PATH"
echo ""

# リストア手順を表示
log_info "To restore this backup, run:"
if [ "$COMPRESS" = true ]; then
    echo "  gunzip -c $BACKUP_PATH | docker compose exec -T postgres psql -U catchup"
else
    echo "  docker compose exec -T postgres psql -U catchup < $BACKUP_PATH"
fi
echo ""
