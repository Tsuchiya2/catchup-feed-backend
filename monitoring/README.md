# Monitoring Guide

catchup-feed プロジェクトのモニタリング・メトリクス設定ガイド

## 概要

このプロジェクトは Prometheus + Grafana による包括的なモニタリング環境を提供します。

### 構成

```
┌─────────────┐
│  catchup-   │
│    API      │ ──┐
└─────────────┘   │
                  │ /metrics
                  ├──────────────> ┌─────────────┐
┌─────────────┐   │                │ Prometheus  │
│  catchup-   │   │                │ (収集/保存) │
│   Worker    │ ──┘                └─────────────┘
└─────────────┘                            │
                                           │ データソース
                                           ▼
                                    ┌─────────────┐
                                    │   Grafana   │
                                    │ (可視化)    │
                                    └─────────────┘
```

## クイックスタート

### 1. 起動

```bash
# すべてのサービスを起動（Prometheus + Grafana を含む）
docker compose up -d

# ログ確認
docker compose logs -f prometheus
docker compose logs -f grafana
```

### 2. アクセス

#### Prometheus UI
- URL: http://localhost:9090
- 用途: メトリクスのクエリ、アラートの確認

#### Grafana Dashboard
- URL: http://localhost:3000
- デフォルトログイン:
  - Username: `admin`
  - Password: `admin` (初回ログイン後に変更)
- ダッシュボード: "Catchup Feed - Overview" が自動でインポート済み

#### API メトリクスエンドポイント
- URL: http://localhost:8080/metrics
- 用途: Prometheus形式のメトリクス確認

## メトリクス一覧

### HTTP メトリクス

| メトリクス名 | 型 | 説明 |
|------------|---|------|
| `http_requests_total` | Counter | HTTPリクエスト総数 (method, path, status) |
| `http_request_duration_seconds` | Histogram | HTTPリクエスト処理時間 |
| `http_request_size_bytes` | Histogram | HTTPリクエストサイズ |
| `http_response_size_bytes` | Histogram | HTTPレスポンスサイズ |
| `http_active_connections` | Gauge | アクティブ接続数 |

### ビジネスメトリクス

| メトリクス名 | 型 | 説明 |
|------------|---|------|
| `articles_total` | Gauge | 記事総数 |
| `sources_total` | Gauge | ソース総数 |
| `articles_fetched_total` | Counter | 取得した記事数 (source) |
| `articles_summarized_total` | Counter | 要約した記事数 (status) |
| `summarization_duration_seconds` | Histogram | 要約処理時間 |

### データベースメトリクス

| メトリクス名 | 型 | 説明 |
|------------|---|------|
| `db_query_duration_seconds` | Histogram | DBクエリ実行時間 (operation) |

## アラート

Prometheus アラートは `monitoring/alerts/catchup-alerts.yml` で定義されています。

### 主要アラート

| アラート名 | 深刻度 | 説明 | 閾値 |
|----------|-------|------|------|
| `APIServerDown` | Critical | APIサーバーがダウン | 1分以上ダウン |
| `HighErrorRate` | Warning | エラー率が高い | 5% 以上 |
| `HighResponseTime` | Warning | レスポンスタイムが長い | p95 > 1秒 |
| `HighSummarizationFailureRate` | Warning | 要約失敗率が高い | 20% 以上 |
| `ArticleFetchStalled` | Warning | 記事取得が停止 | 30分間取得なし |
| `SlowDatabaseQueries` | Warning | DBクエリが遅い | p95 > 1秒 |

### アラート確認

```bash
# Prometheus UI でアラート確認
open http://localhost:9090/alerts
```

## Grafana ダッシュボード

### "Catchup Feed - Overview" ダッシュボード

自動インポートされる包括的なダッシュボードには以下のパネルが含まれます：

#### システムメトリクス
- HTTPリクエスト率
- HTTPレスポンスタイム (p95)
- HTTPステータスコード分布
- アクティブ接続数

#### ビジネスメトリクス
- 記事総数
- ソース総数
- 記事取得率（ソース別）
- 要約成功率
- 要約処理時間

#### データベースメトリクス
- クエリ実行時間（操作別）

### カスタムダッシュボードの追加

1. Grafana UI でダッシュボードを作成
2. JSON エクスポート
3. `monitoring/grafana/dashboards/` に保存
4. 自動的にインポートされます（30秒以内）

## Prometheus クエリ例

### HTTPリクエスト率（5分平均）
```promql
sum(rate(http_requests_total[5m])) by (method)
```

### エラー率
```promql
sum(rate(http_requests_total{status=~"5.."}[5m]))
/
sum(rate(http_requests_total[5m]))
```

### p95 レスポンスタイム
```promql
histogram_quantile(0.95,
  sum(rate(http_request_duration_seconds_bucket[5m])) by (le)
)
```

### 要約成功率
```promql
sum(rate(articles_summarized_total{status="success"}[10m]))
/
sum(rate(articles_summarized_total[10m]))
```

## トラブルシューティング

### Prometheus がメトリクスを収集できない

**症状**: Grafana でデータが表示されない

**確認**:
```bash
# APIサーバーのメトリクスエンドポイントが動作しているか
curl http://localhost:8080/metrics

# Prometheus が API サーバーに到達できるか
docker compose exec prometheus wget -O- http://app:8080/metrics

# Prometheus のターゲット状態確認
open http://localhost:9090/targets
```

**解決策**:
- APIサーバーが起動しているか確認
- ネットワーク設定を確認
- Prometheus 設定ファイルの構文エラーチェック

### Grafana ダッシュボードが表示されない

**症状**: ダッシュボードが空または「No Data」

**確認**:
```bash
# データソースが正しく設定されているか
# Grafana UI: Configuration → Data sources → Prometheus

# データソースの接続テスト
# Grafana UI: Data sources → Test ボタン
```

**解決策**:
- Prometheusが起動しているか確認
- データソースURLが正しいか確認（`http://prometheus:9090`）
- 時間範囲を調整（右上の時刻選択）

### メトリクスが記録されない

**症状**: 特定のメトリクスが 0 または表示されない

**確認**:
```bash
# アプリケーションログでエラー確認
docker compose logs app | grep -i metric
docker compose logs worker | grep -i metric

# メトリクスエンドポイントで値を確認
curl http://localhost:8080/metrics | grep articles_total
```

**解決策**:
- アプリケーションが正常に動作しているか確認
- メトリクス記録関数が正しく呼ばれているか確認
- アプリケーションを再起動

## パフォーマンスチューニング

### Prometheus ストレージ

デフォルトでは30日間のデータを保持します。

```yaml
# compose.yml で調整
command:
  - '--storage.tsdb.retention.time=30d'  # 保持期間
  - '--storage.tsdb.retention.size=10GB'  # 最大サイズ
```

### スクレイプ間隔

デフォルトは15秒です。

```yaml
# monitoring/prometheus.yml で調整
global:
  scrape_interval: 15s  # 全体のデフォルト

scrape_configs:
  - job_name: 'catchup-api'
    scrape_interval: 10s  # ジョブ別に調整
```

### リソース制限

```yaml
# compose.yml でリソース制限を調整
deploy:
  resources:
    limits:
      cpus: '1.0'
      memory: 512M
```

## ベストプラクティス

### 1. メトリクスの命名規則

- **Counter**: `_total` で終わる（例: `http_requests_total`）
- **Histogram/Summary**: `_duration_seconds`, `_size_bytes` など単位を含む
- **Gauge**: 現在の状態を表す名前（例: `active_connections`）

### 2. ラベルの使用

```go
// Good: 低カーディナリティ
http_requests_total{method="GET", path="/api/articles", status="200"}

// Bad: 高カーディナリティ（避ける）
http_requests_total{user_id="12345", article_id="67890"}
```

### 3. ダッシュボード設計

- **重要メトリクスを上部に配置**
- **関連メトリクスをグループ化**
- **適切な時間範囲を選択**（デフォルト: 1時間）

### 4. アラート設計

- **適切な閾値設定**: 誤検知を避ける
- **適切な評価期間**: 一時的なスパイクを無視
- **明確なアラートメッセージ**: 対応方法を含む

## 本番環境での推奨事項

### 1. セキュリティ

```yaml
# .env で認証情報を変更
GRAFANA_ADMIN_USER=your_admin
GRAFANA_ADMIN_PASSWORD=strong_password_here

# Prometheus に Basic 認証を追加（オプション）
# nginx などのリバースプロキシ経由でアクセス
```

### 2. 外部ストレージ

長期保存が必要な場合は、Thanos や Cortex を検討：

```bash
# Thanos を使った長期保存の例
# (別途設定が必要)
```

### 3. アラート通知

Alertmanager を追加して Slack/Email 通知を設定：

```yaml
# prometheus.yml に追加
alerting:
  alertmanagers:
    - static_configs:
        - targets:
          - 'alertmanager:9093'
```

### 4. バックアップ

```bash
# Prometheus データのバックアップ
docker compose exec prometheus \
  tar czf /prometheus/backup.tar.gz /prometheus/data

# Grafana ダッシュボードのバックアップ
# UI からエクスポートまたは API 使用
```

## 参考リンク

- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
- [Prometheus Best Practices](https://prometheus.io/docs/practices/)
- [Go Prometheus Client](https://github.com/prometheus/client_golang)
