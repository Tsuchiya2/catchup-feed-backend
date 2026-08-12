#!/bin/bash
# ============================================================
# PostgreSQL データベースリストアスクリプト
# ============================================================
# バックアップファイルからデータベースを復元
#
# 使用方法:
#   ./scripts/restore-db.sh BACKUP_FILE
#
# 警告:
#   既存のデータはすべて削除されます！
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

# 引数チェック
if [ $# -ne 1 ]; then
    log_error "Usage: $0 BACKUP_FILE"
    exit 1
fi

BACKUP_FILE="$1"

if [ ! -f "$BACKUP_FILE" ]; then
    log_error "Backup file not found: $BACKUP_FILE"
    exit 1
fi

echo ""
echo "============================================================"
echo "  🔄 PostgreSQL Database Restore"
echo "============================================================"
echo "  Backup file: $BACKUP_FILE"
echo "============================================================"
echo ""

log_warn "⚠️  WARNING: This will DELETE all existing data!"
log_warn "⚠️  Make sure you have a recent backup before proceeding."
echo ""

# 確認プロンプト
read -p "Are you sure you want to restore? (yes/no): " CONFIRM
if [ "$CONFIRM" != "yes" ]; then
    log_info "Restore cancelled"
    exit 0
fi

# Docker Compose が起動しているか確認
cd "$PROJECT_ROOT"
if ! docker compose ps postgres | grep -q "Up"; then
    log_error "PostgreSQL container is not running!"
    log_info "Start with: docker compose up -d postgres"
    exit 1
fi

# 接続中のクライアントを切断
log_info "Disconnecting all clients..."
docker compose exec -T postgres psql -U catchup-feed -d postgres -c \
    "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'catchup-feed' AND pid <> pg_backend_pid();" \
    || log_warn "Could not disconnect all clients"

# データベースを削除して再作成
log_info "Dropping and recreating database..."
docker compose exec -T postgres psql -U catchup-feed -d postgres <<-EOSQL
    DROP DATABASE IF EXISTS "catchup-feed";
    CREATE DATABASE "catchup-feed";
EOSQL

# リストア実行
log_info "Restoring from backup..."

if [[ "$BACKUP_FILE" == *.gz ]]; then
    # gzip圧縮ファイル
    log_info "Decompressing and restoring..."
    if gunzip -c "$BACKUP_FILE" | docker compose exec -T postgres psql -U catchup-feed -d catchup-feed; then
        log_success "Restore completed"
    else
        log_error "Restore failed!"
        exit 1
    fi
else
    # 非圧縮ファイル
    if docker compose exec -T postgres psql -U catchup-feed -d catchup-feed < "$BACKUP_FILE"; then
        log_success "Restore completed"
    else
        log_error "Restore failed!"
        exit 1
    fi
fi

# データベースサイズ確認
log_info "Checking database size..."
DB_SIZE=$(docker compose exec -T postgres psql -U catchup-feed -d catchup-feed -t -c \
    "SELECT pg_size_pretty(pg_database_size('catchup-feed'));" | tr -d ' ')
log_info "Database size: $DB_SIZE"

# テーブル数確認
TABLE_COUNT=$(docker compose exec -T postgres psql -U catchup-feed -d catchup-feed -t -c \
    "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';" | tr -d ' ')
log_info "Tables: $TABLE_COUNT"

echo ""
log_success "Database restored successfully!"
log_warn "Please restart the application to ensure consistency"
echo "  docker compose restart server worker"
echo ""
