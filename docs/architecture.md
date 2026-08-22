# アーキテクチャ

**対象**: catchup-feed-backend(Go 1.27.0 単一モジュール)
**最終更新**: 2026-08-13

このドキュメントは**現行実装**のアーキテクチャを記述します。Phase 別の要件・決定ログ(D-xx / C-xx)は親リポジトリの `docs/` が正で、記述が食い違う場合は設計書を優先してください。ディレクトリとファイル単位の責務は [repository-structure.md](repository-structure.md) にあります。

---

## 目次

1. [システム概要](#1-システム概要)
2. [設計原則](#2-設計原則)
3. [レイヤーアーキテクチャ](#3-レイヤーアーキテクチャ)
4. [データフロー](#4-データフロー)
5. [データモデル](#5-データモデル)
6. [プロセス間連携](#6-プロセス間連携)
7. [セキュリティ](#7-セキュリティ)
8. [縮退設計](#8-縮退設計)
9. [技術選定](#9-技術選定)
10. [初代からの変更点](#10-初代からの変更点)
11. [既知の制約とトレードオフ](#11-既知の制約とトレードオフ)

---

## 1. システム概要

技術情報(RSS / YouTube / ポッドキャスト / 書籍 PDF)を収集・要約し、毎朝 10〜15 分のラジオ番組(mp3)を生成してポッドキャストアプリに配信する、単一ユーザー向けの学習システムです。最適化目標は「配信記事数」ではなく **理解の定着** で、番組から生成したクイズを spaced repetition で翌朝の番組に注入します。

### 実行構成

単一 Go モジュールから **3 つの常用バイナリ**と 2 つの補助バイナリをビルドします。**内部 HTTP / RPC は持たず**、プロセス間連携はすべて PostgreSQL(`jobs` テーブル + 状態テーブル)経由です。

| バイナリ | 配置 | 役割 | 規模 |
|---|---|---|---|
| `cmd/server` | Pi 5(常駐) | 公開フィード配信・管理 API(JWT)・tailnet 限定の私的フィード。起動時に冪等マイグレーションを自動適用 | 561 行 |
| `cmd/worker` | Pi 5(常駐) | cron でクロール → 本文抽出 → 要約。`jobs` テーブルのコンシューマ | 393 行 |
| `cmd/radio` | M3 Mac(夜間バッチ) | 記事選定 → 台本生成 → VOICEVOX 合成 → ffmpeg 結合 → rsync 転送 | 185 行 |
| `cmd/hash-password` | 開発機 | 管理者パスワードの bcrypt ハッシュ生成 | 81 行 |
| `cmd/crawl-once` | 開発機 | 単発クロールの開発ユーティリティ | 167 行 |

`cmd/` はいずれも薄く、依存の組み立て(composition root)とプロセスのライフサイクル管理に徹します。ロジックは `internal/` にあります。

### ホスト配置

```text
┌──────────── Raspberry Pi 5(常時稼働)──────────────┐
│  server  : 公開フィード配信 / 管理 API / 私的フィード │
│  worker  : クロール・要約・ジョブ処理(cron 常駐)     │
│  PostgreSQL + mp3 アーカイブ(episodes/)             │
└──────────────────────────────────────────────────┘
          ▲ Tailscale(rsync / PostgreSQL 接続)
┌──────────── M3 MacBook Pro(夜間バッチ)────────────┐
│  radio   : 台本構成 → VOICEVOX → ffmpeg 結合        │
│  VOICEVOX Engine(現行話者: No.7 アナウンス / D-2)   │
│  Ollama(要約・書籍のローカル LLM フォールバック)     │
└──────────────────────────────────────────────────┘

公開経路: Cloudflare Tunnel → Pi server
  - pulse.catchup-feed.com         → Next.js ダッシュボード(JWT 保護)
  - radio.catchup-feed.com/feeds/* → 公開フィード(トークン認証)
私的経路: Tailscale(tailnet 内のみ、認証は物理境界)
  - pi.tailnet:8081/private/feed.xml
```

---

## 2. 設計原則

単一ユーザーがゼロ円で運用する自宅ホスティングを前提に「右サイズ」で設計しています。

| 原則 | 内容 | 実装上の帰結 |
|---|---|---|
| **単一ユーザー右サイズ** | 冗長化・可観測性基盤・内部 RPC を持たない | プロセス間連携は `jobs` テーブルのみ。メトリクス基盤なし(フォールバック発生は `summaries.provider` で事後観測) |
| **ゼロ円運用** | 有料 API・有料 SaaS を使わない | 要約は無料枠 API → ローカル LLM のフォールバック連鎖。ホスティングは自宅 Pi + Cloudflare Tunnel |
| **縮退許容** | 「壊れない」より「壊れても翌日勝手に戻る」 | Mac 不在 → エピソード欠番、無料 API 全滅 → Ollama、TTS 障害 → 当日スキップ + 通知 |
| **プライバシー分界** | クラウド API に流してよいのは公開記事とその要約のみ | 書籍・私的データはローカル LLM(Ollama)でのみ処理。学習状態は JWT 必須 API の背後 |

---

## 3. レイヤーアーキテクチャ

本システムは **管理 API 側は Clean Architecture、それ以外は用途別パッケージ + Go 流のポート**というハイブリッド構成です。両者に共通するのは「依存は常に内向き」「外部との境界は必ず interface」の 2 点です。

### 3.1 server 側 — Clean Architecture

```text
外 ─────────────────────────────────────────────────→ 内
handler/http/*  →  usecase/*  →  repository/*(interface)  →  domain/entity
                                        ▲
infra/adapter/persistence/postgres ─────┘ 実装を cmd/server で注入
```

| 層 | ディレクトリ | 責務 | 行数 |
|---|---|---|---|
| Domain | `internal/domain/entity` | エンティティ・不変条件・URL 検証(SSRF 対策を含む)。**標準ライブラリ以外に依存しない** | 516 |
| Port | `internal/repository` | 永続化インターフェース 14 本 + 境界を越える構造体。実装は持たない | 791 |
| UseCase | `internal/usecase/*` | アプリケーションロジック(article / source / subscriber / viewer / accesslog / learning / book / fetch) | 3,394 |
| Presentation | `internal/handler/http/*` | ルーティング・DTO・JWT 認証・CORS/CSP・レート制限 | 5,720 |
| Infrastructure | `internal/infra/*` | PostgreSQL アダプタ・HTTP フェッチャ・要約 LLM クライアント・マイグレーション | 5,703 |

**依存性逆転**: `internal/repository` の 14 インターフェースは内側にあり、`internal/infra/adapter/persistence/postgres` が外側からそれを実装します。usecase は具象型を知りません。

**クロール系のポートは usecase 側**: `internal/usecase/fetch` は `ContentFetcher` / `FeedFetcher` / `Summarizer` / `ProviderSummarizer` / `VideoDescriber` / `HTMLFetcher` の 6 インターフェースを定義し、`internal/infra/fetcher` と `internal/infra/scraper` がそれを import して実装します(依存の向きは外 → 内)。

### 3.2 用途別パッケージ + consumer-side interface

Clean Architecture の 4 層に載せていないパッケージ群です。層ではなく**用途**で切り、外部依存は「必要なメソッドだけを利用側で定義する」Go 流のポート(consumer-side interface)で抽象化します。

「利用」列は `go list` で確認した実際の import 元です。**`feed` は server 側のパッケージ**であり、radio からは参照しません — 配信ハンドラを `cmd/server` が使い、`cmd/worker` は `LoadConfig()` で `FEED_AUDIO_DIR`(mp3 の掃除先)と `FEED_PRIVATE_BASE_URL`(通知メールのリンク)を読むためだけに触ります。

| ディレクトリ | 利用 | 責務 | 行数 | interface |
|---|---|---|---|---|
| `internal/radio` | radio | 番組生成パイプライン(選定 → 台本 → 合成 → 結合 → 転送 → 登録) | 1,849 | 10 |
| `internal/script` | radio | LLM 台本生成・クイズ生成・番組の定型句(D-37) | 1,440 | 2 |
| `internal/tts` | radio | VOICEVOX 合成・無音生成・ffmpeg 結合・WAV 操作 | 771 | 0 |
| `internal/feed` | **server** + worker | RSS XML 生成・公開/私的フィードの配信ハンドラ・アートワーク | 817 | 0 |
| `internal/jobs` | worker + radio | `jobs` テーブルのコンシューマ(worker)とジョブ投入(radio) | 666 | 3 |
| `internal/learning` | server + radio | SRS 遷移の純関数・JST 放送日ヘルパ・クイズパラメータ | 433 | 0 |
| `internal/notify` | worker | 管理者向けメール通知(SMTP)。D-29 でメールに一本化、D-32 で友人向けメールを廃止 | 324 | 2 |

`internal/radio/pipeline.go` は自分に必要なメソッドのみを定義します。

```go
type ArticleSource interface { ListSummarizedSince(ctx, since, limit) ([]repository.RadioArticle, error) }
type EpisodeStore  interface { ListByKind(...); CountByKindSince(...); Create(...) }
type JobQueue      interface { Enqueue(ctx, kind, payload, runAfter) (int64, error) }
```

`repository.EpisodeRepository`(メソッド多数)ではなく 3 メソッドの `EpisodeStore` に依存することで、インターフェース分離原則を満たしつつテスト時の偽物実装も小さく済みます。外部プロセス実行も `Transferer` / `RunFunc`(`internal/radio/transfer.go`)で差し替え可能です。

**`internal/learning` の位置づけ**: SRS 遷移(ラダー `1,7,30` 日)と JST 放送日計算の純関数群で、`radio` と `server` の両方が使う共有コアです。**外側(radio / server / handler / repository)へは依存させない**という制約を設けており、実質的な第 2 のドメイン層として機能します。

### 3.3 依存ルールと検証結果

`go list` で全パッケージの import を検査した結果です(2026-08-13 時点)。

| ルール | 結果 |
|---|---|
| `handler` → `infra` の直接参照(層飛ばし) | **0 件** |
| `usecase` / `domain` / `repository` → `handler` / `infra` の逆流 | **0 件** |
| `domain/entity` の外部依存 | **0 件**(標準ライブラリのみ) |
| `domain/entity` の ORM / DB タグ | **0 件**(json タグは `job.go` の 5 箇所のみ。`jobs.payload` の JSONB 用) |
| `internal/learning` の外側依存 | **0 件**(`pkg/config` のみ) |

検証コマンド(いずれも出力が空であること):

```bash
# 1. 層飛ばし: handler が infra を直接 import していないか
go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./... \
  | grep '^catchup-feed/internal/handler' | grep 'catchup-feed/internal/infra'

# 2. 逆流: 内側の層が外側を import していないか
go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./... \
  | grep -E '^catchup-feed/internal/(usecase|domain|repository)' \
  | grep -E 'catchup-feed/internal/(handler|infra)'

# 3. domain/entity が標準ライブラリ以外に依存していないか
go list -f '{{join .Imports "\n"}}' ./internal/domain/entity | grep -E '\.|catchup-feed'

# 4. learning が外側に依存していないか(pkg/config のみ許容)
go list -f '{{join .Imports "\n"}}' ./internal/learning \
  | grep catchup-feed | grep -v '^catchup-feed/pkg/config$'
```

1 と 2 は `^` でパッケージ側を固定するのが要点です。これを外すと handler 同士の import(`handler/http/article` → `handler/http/auth` など、層内の正当な参照)が拾われ、違反があるように見えます。

依存方向を機械的に強制する lint(depguard 等)は導入していません。`.golangci.yml` は `errcheck` / `govet` / `staticcheck` / `unused` / `ineffassign` のみで、レイヤー規律はレビューで担保しています。

### 3.4 意図的な逸脱とその理由

教科書的な Clean Architecture から外れている箇所と、その判断根拠です。

| 逸脱 | 内容 | 理由 |
|---|---|---|
| **配信ハンドラが `handler/` の外** | `internal/feed/server.go` は `handlePublicFeed` / `serveAudio` と認証ミドルウェアを持つが `handler/http/` にない | フィード配信は「管理 API とは別のリスナー・別の認証方式・別のライフサイクル」を持つ独立した口。管理 API のミドルウェアチェーンとは共有しないため、層で括るより用途で括る方が変更時の影響範囲が読める |
| **インフラが `infra/` の外** | `internal/tts`(VOICEVOX HTTP / ffmpeg)、`internal/notify`(SMTP)は実体としてインフラ | いずれも radio / worker 専用で、server 側からは一切参照しない。`infra/` に置くと「3 バイナリ共通の基盤」に見えてしまう |
| **オーケストレーションと I/O の同居** | `internal/radio` は選定・構成のロジックと `os.WriteFile` / `exec.Command`(rsync)を同一パッケージに持つ | 分割しても利用者が radio だけなら間接層が増えるだけ。副作用は `Transferer` / `RunFunc` として interface 化してあり、テスト可能性は確保している |
| **repository の型が handler まで到達** | `usecase/article.Service.ListWithSource()` は `[]repository.ArticleWithSource` を返し、`handler/http/learning/dto.go` は `repository.PendingReview` を DTO 化する | 層ごとに DTO を再定義すると、単一ユーザー・単一フロントエンドの規模では変換コードが増えるだけ。境界の型は `repository` パッケージに集約し、そこが唯一の定義元であることを規約とした |
| **認証実装が Presentation 層** | bcrypt 検証と JWT 発行が `handler/http/auth/` にある(`service/auth` はポート 44 行のみ) | 管理者は環境変数 + bcrypt ハッシュの単一アカウントで、DB レコードを持たない(C-7)。永続化を伴わないため usecase 層に置く必然性が薄い。viewer 認証は `usecase/viewer` 経由で DB 照合する |
| **ドメインモデルが薄い** | `entity` のメソッドは 5 個(`Source.Validate` / `FeedToken.IsRevoked` / `IsActive` ×2 / `ValidationError.Error`)。残りはパッケージ関数(`ValidateURL` / `GenerateFeedToken` 等)で、ビジネスルールは usecase に集中 | 本システムのドメインは「収集した記事を並べて読み上げる」であり、エンティティ単体の不変条件が少ない。無理にリッチモデル化せず、SSRF 検証など**外部入力に対する不変条件のみ** entity に置く方針 |

### 3.5 コード規模

| 区分 | 行数 | 比率 |
|---|---|---|
| Clean Architecture の 4 層(domain / repository / usecase / handler / infra) | 16,124 | 62% |
| 用途別パッケージ(radio / script / tts / feed / jobs / learning / notify) | 6,300 | 24% |
| 共通(common / internal/pkg / service / utils / config) | 1,432 | 6% |
| `cmd/`(composition root) | 1,387 | 5% |
| `pkg/`(公開パッケージ: config / security/csp) | 770 | 3% |
| **本体合計** | **26,013** | 100% |
| テスト(162 ファイル) | 48,283 | 本体比 1.86 倍 |

interface は全体で **47 本**(`repository` 14 / `radio` 10 / `usecase/fetch` 6 / `handler/middleware` 4 / `jobs` 3 / `auth` `notify` `script` 各 2 / その他 4)。テスト内のモック・スタブ定義は 65 個で、DIP が実際にテスト容易性へ還元されています。

---

## 4. データフロー

### 4.1 クロールと要約(worker / Pi・毎時)

```text
robfig/cron(毎時)
  ├→ usecase/fetch.Service.CrawlAllSources
  │    ├→ infra/scraper(gofeed)        : RSS / Atom をパース
  │    ├→ 14 日より古い item を破棄     : 全 kind 共通のバックログカットオフ(D-15 / D-15b)
  │    ├→ infra/fetcher(go-readability): 本文抽出。リダイレクトごとに SSRF 検証
  │    ├→ infra/summarizer.Chain       : Gemini → Groq → Ollama の順に試行
  │    └→ ArticleRepository.CreateWithSummary : articles + summaries を原子的に INSERT
  └→ usecase/fetch.Service.SweepUnsummarized
       └→ 挿入後に content が埋まった記事(文字起こし)を要約して summaries へ UPSERT
```

要約は記事と同一トランザクションで永続化します(要約に失敗した記事は INSERT ごとロールバックされ、URL が未知のまま次の毎時クロールで再試行される)。`summaries.provider` に採用プロバイダを記録し、フォールバックの発生を事後観測します。

kind ごとに取り込み経路が分かれます。

| `sources.kind` | 取り込み |
|---|---|
| `rss` | go-readability で本文抽出 → 要約 |
| `youtube` | 第1段: Gemini に動画 URL を直接入力(1サイクル最大 3 件)。失敗時は `transcribe` ジョブへ |
| `podcast` | enclosure の音声 URL を `transcribe` ジョブへ。記事は content なしで先に INSERT |
| `newsletter` | 1 item = 1 号として号内リンクを展開し、先頭 N 件(既定 10)を取得・要約 |

`transcribe` ジョブは Pi の worker がハンドラを登録せず、Mac の catchup-ai(Python)だけが取得します。クロール順は `youtube` / `podcast` を `rss` より先に処理します — 要約詰まりで末尾のソースが毎サイクル未到達になる本番障害への対策です。

### 4.2 番組生成(radio / Mac・04:30 JST)

```text
cmd/radio
  └→ radio.Pipeline.Run
       1. ArticleSource.ListSummarizedSince  : 前回エピソード以降の要約済み記事(上限 200)
       2. LearningStore.AutoResolve/ListDue  : 48h 未採点を自動繰り上げ、当日出題分を取得
       3. ScriptGenerator.GenerateEpisode    : 台本 + クイズ草稿を同一 LLM コールで生成(D-19)
       4. Synthesizer.SynthesizeScript       : VOICEVOX でセグメント別に合成
       5. ffmpeg 結合(loudnorm / 64kbps mono / 44.1kHz)+ 前後にジングルを付与
       6. Transferer.Transfer                : rsync over Tailscale で Pi へ転送
       7. EpisodeStore.Create                : episodes / segments を INSERT
       8. JobQueue.Enqueue                   : regenerate_feed / notify_episode を投入
       9. 私的版(private twin)を best-effort で生成 : ニュース wav を公開版と共用し、
          復習コーナー(問題 → 3 秒無音 → 答え)・週次振り返り・書籍コーナーを追加
```

公開エピソードの確定後に私的版を作ります。私的版の失敗は公開版を巻き添えにしません(縮退方向は「公開版は出す、私的版のみ諦める」)。同日再実行は上書きせず `rev2` 以降の別ファイルになります。

記事ゼロかつ出題対象・アクティブ書籍なしの日は `ErrNoArticles` で正常終了します(D-1: 欠番は障害ではない)。記事ゼロでも出題対象があれば私的版のみを配信します。

### 4.3 フィード配信(server / Pi)

```text
ポッドキャストアプリ
  └→ Cloudflare Tunnel → server:8080
       └→ feed.Server.RegisterPublic が登録したルート
            ├→ トークン検証(URL 埋め込み・SHA-256 照合)
            ├→ FeedTokenRepository / EpisodeRepository
            ├→ feed_access_logs へ記録
            └→ RSS XML または mp3(os.OpenRoot でディレクトリ外アクセスを遮断)
```

私的フィードは tailnet にバインドした別リスナー(`:8081`)で配信し、CORS / CSP / JWT を適用しません。ネットワーク境界そのものを認証とみなす設計です(C-5)。

### 4.4 学習ループ(Phase 3)

```text
[radio]  番組生成時 : 放送記事からクイズを生成 → learning_items へ INSERT
                     出題した item を review_logs に記録(asked)
[server] ダッシュボード: GET /learning/reviews/pending → ○△× を POST で採点
[learning] 純関数    : 採点結果から次回出題日を決定(ラダー 1 / 7 / 30 日)
[radio]  翌朝以降    : ListDue で期日到来分を取得し、番組の「復習コーナー」に音声注入
```

48 時間採点されなかった出題は `result=auto` として自動繰り上げされ、キューが飽和しません(D-17)。

---

## 5. データモデル

PostgreSQL 14 テーブル。マイグレーションは `internal/infra/db.MigrateUp` の冪等 SQL を `cmd/server` 起動時に自動適用します(専用バイナリなし)。

| テーブル | 用途 |
|---|---|
| `sources` | 収集元。`kind` = `rss` / `youtube` / `podcast` / `newsletter`。**論理削除**(`articles` が FK 参照するため物理削除できない) |
| `articles` | 収集した記事。URL で重複排除。`content` が NULL の間は文字起こし待ち |
| `summaries` | 記事の要約。`provider` に採用した LLM を記録(縮退の事後観測用) |
| `episodes` | 生成した番組。`feed_kind` で公開 / 私的を区別 |
| `segments` | 番組内のセグメント(記事・クイズ・書籍コーナー) |
| `jobs` | プロセス間連携のジョブキュー |
| `feed_tokens` | 友人向け配信トークン。**平文は保存せず SHA-256 のみ** |
| `feed_access_logs` | フィード / mp3 のアクセスログ |
| `subscribers` | 友人(配信先) |
| `viewers` | ダッシュボード閲覧アカウント(読み取り専用ロール、D-27) |
| `books` | 取り込んだ書籍 PDF のメタデータ |
| `book_chunks` | 書籍のチャンク + ベクトル(pgvector、catchup-ai が書き込み) |
| `learning_items` | 復習アイテム(クイズ) |
| `review_logs` | 出題・採点の履歴。SRS の遷移元データ |

---

## 6. プロセス間連携

内部 RPC を持たない代わりに、`jobs` テーブルを単一の連携点にしています。

```text
radio(Mac) ──INSERT──→ [ jobs ] ←──ClaimNext── worker(Pi)
                          │
              kind ごとに登録された Handler へディスパッチ
```

`internal/jobs.Consumer` は `Handlers map[string]Handler` に登録された kind のみを `ClaimNext` で取得します。ハンドラ未登録で起動した場合は「空の kinds が全ジョブを掴む」事故を防ぐため起動を拒否します。起動時には自プロセスが担当する kind の `running` ジョブを `RequeueRunning` で復帰させ、異常終了後の取りこぼしを回収します。

| kind | 生成元 | 処理内容 |
|---|---|---|
| `regenerate_feed` | radio | 現在は no-op。feed.xml はリクエスト毎に描画するため再生成対象のキャッシュが存在しない。将来キャッシュを導入する場合にハンドラの変更だけで済むよう kind は残している |
| `notify_episode` | radio(**公開エピソードのみ**) | 管理者宛メールにタイトル + ショーノート + 私的エピソード URL を送る(D-29 でメールに一本化、D-32 で友人向けメールを廃止)。私的版はジョブを積まない — ショーノートに学習コンテンツを含むため(§12-7) |
| `notify_error` | radio | 管理者宛の障害通知(best-effort。通知失敗を再通知するループを作らないため常に成功扱い) |
| `cleanup_old_media` | worker(cron) | 45 日より古い mp3 の削除 + どのエピソードからも参照されない孤児 mp3(rsync 成功後に INSERT が失敗した残骸)の削除。孤児は更新から 48 時間経過したものだけを対象にする(D-4) |
| `transcribe` | worker | 音声・動画の文字起こし依頼。**Pi の worker はハンドラを登録せず**、Mac の catchup-ai だけが claim する |
| `book_ingest` | server | 書籍 PDF の取り込み依頼。同上(取り込みは Ollama を使うためローカル LLM 側でしか実行できない) |

---

## 7. セキュリティ

### 7.1 認証・認可

| 対象 | 方式 |
|---|---|
| 管理 API | JWT(HS256)を HttpOnly Cookie で保持。管理者は環境変数 + bcrypt ハッシュの単一アカウント(DB レコードを持たない、C-7) |
| ダッシュボード閲覧者 | `viewers` テーブルの読み取り専用ロール。リクエスト毎に DB を再検証し、許可リスト(`GET /sources` / `GET /auth/me`)のみ通す(D-27) |
| フィード配信 | URL 埋め込みの不透明トークン。`crypto/rand` 32 byte → base64url。**DB には SHA-256 ハッシュのみ保存**し平文は保持しない |
| 私的フィード | 認証なし。tailnet へのバインドという物理境界を認証とみなす(C-5) |

トークン不一致・失効・購読停止はすべて **404 に統一**し、レスポンスからトークンの存在有無が漏れないようにしています。ログアウト(`POST /auth/logout`)は POST 限定で、`<img src>` による反射 GET での強制ログアウト(GET CSRF)を防いでいます。

### 7.2 ミドルウェアチェーン

公開リスナー(`cmd/server/main.go:applyMiddleware`)の適用順(外 → 内):

```text
CORS → RequestID → Recover → Logging → BodyLimit(1MB / PDF は 101MB) → CSP → routes
```

私的リスナーは `RequestID → Recover → Logging` のみで、CORS / CSP / 認証を適用しません。

### 7.3 SSRF 防御

`internal/infra/fetcher` は RSS 取得・本文抽出の両方で、**リダイレクトのホップごとに**接続先を検証します(`SSRFCheckRedirect`)。プライベート IP 帯への接続を拒否し、サイズ上限とリダイレクト回数上限を課します。URL 自体の妥当性検証(`entity.ValidateURL`)はドメイン層にあり、ソース登録時にも同じ規則が適用されます。

### 7.4 レート制限

インメモリの per-IP トークンバケットです(単一プロセス・単一ユーザーのため分散カウンタを持たない)。

| 対象 | 上限 |
|---|---|
| `/auth/token` | 5 req/min |
| 検索エンドポイント | 100 req/min |
| 公開フィード | 60 req/min |

送信元 IP は `middleware.IPExtractor` で決定します。Cloudflare Tunnel 配下では信頼できるプロキシ段数を設定して `X-Forwarded-For` の詐称を防ぎます。エントリは定期的に掃除してメモリの無制限増加を防ぎます。

### 7.5 CSP / CORS

CSP は `pkg/security/csp` のビルダーで生成し、既定は `StrictPolicy()`、`/swagger/` のみ `SwaggerUIPolicy()` を適用します。`CSP_REPORT_ONLY` で Report-Only 運用に切り替え可能です。CORS は許可オリジンを環境変数で明示し、起動時にログへ出力します。

### 7.6 プライバシー分界

| データ | 処理先 |
|---|---|
| 公開記事とその要約 | クラウド API(Gemini / Groq)可 |
| 書籍 PDF・その要約・私的メモ | **ローカル LLM(Ollama)のみ** |
| 学習状態(理解度・採点履歴) | JWT 必須 API の背後。公開フィードには一切載せない |

公開エピソードにはクイズ・書籍コーナーを含めず、私的フィード側にのみ注入します(§12-1: 公開エピソードの完全不変)。

---

## 8. 縮退設計

「壊れない」ことではなく「壊れても翌日勝手に戻る」ことを設計目標にしています。

| 障害 | 縮退動作 | 復旧 |
|---|---|---|
| Mac が閉じている / スリープ | その日のエピソードが生成されない(欠番) | 翌日の実行で自動的に再開。前回エピソード以降の記事をまとめて拾う |
| Gemini / Groq の無料枠が尽きる | `summarizer.Chain` が次のプロバイダへフォールバック。最終的に Ollama(ローカル) | API キー未設定のプロバイダは連鎖から自動除外 |
| VOICEVOX Engine が停止 | 当日の番組生成をスキップし `notify_error` を投入 | 翌日の実行で復帰 |
| worker の異常終了 | `running` のまま残ったジョブを起動時に `RequeueRunning` で復帰 | 再実行される |
| 記事ゼロの日 | `ErrNoArticles` で正常終了(障害扱いしない) | — |
| 学習ループの失敗 | クイズ生成失敗は nil 草稿に縮退し、番組本体は生成を続行 | 公開放送は学習ループに依存しない |

---

## 9. 技術選定

| 決定 | 理由 |
|---|---|
| **標準 `net/http` ルーター**(外部ルーター不使用) | Go 1.22 以降の `ServeMux` がメソッド・パスパラメータに対応し、本システムの規模では十分。依存を 1 つ減らせる |
| **pgx/v5 + 生 SQL**(ORM 不使用) | エンティティに ORM タグを持ち込まずに済む(ドメイン層の純粋性)。クエリの実行計画を直接制御できる |
| **PostgreSQL をプロセス間連携に使う** | 単一ユーザー・単一ホスト構成でメッセージブローカーを立てるのは過剰。トランザクションと `jobs` テーブルで十分な信頼性が得られる |
| **要約のフォールバック連鎖** | ゼロ円運用の要。無料枠 API の枯渇を単一障害点にしない。採用プロバイダは `summaries.provider` に記録し事後観測する |
| **VOICEVOX の HTTP API 直叩き** | ラッパーライブラリを挟まず、話者・音響パラメータを直接制御する。合成は radio 専用処理のためライブラリ抽象の恩恵が小さい |
| **マイグレーション専用バイナリを持たない** | 冪等 SQL を server 起動時に適用。デプロイ手順を「コンテナを入れ替える」だけに保つ |
| **メトリクス基盤を持たない** | 単一ユーザーの障害検知は通知(`notify_error`)で足りる。Prometheus / Grafana の運用コストに見合わない |
| **table-driven テスト + testify** | 標準的な Go の書き方に揃え、モック生成ツールへの依存を避ける。interface が小さいため手書きの偽物実装で足りる |

---

## 10. 初代からの変更点

現行 catchup-feed は、RSS を要約して REST API / Discord に流していた**初代 catchup-feed** の作り直しです。初代が抱えていた以下の要素は、単一ユーザーの要件に対して過剰と判断し**すべて削除**しました。

- マイクロサービス分割と内部 gRPC 通信
- サーキットブレーカー / リトライ基盤(`internal/resilience`)
- Prometheus メトリクス / Grafana / OpenTelemetry(`internal/observability`)
- OpenAI・Claude API への依存(有料 API)
- `internal/interface` 層(現行では `internal/handler` に統合)

削減は約 3.8 万行です。このドキュメントの旧版はこれら削除済みコンポーネントを記述していたため、2026-08-13 に現行実装ベースへ全面改訂しました。

---

## 11. 既知の制約とトレードオフ

意図的に受け入れている制約です。要件が変われば再検討の対象になります。

| 制約 | 影響 | 受け入れる理由 |
|---|---|---|
| **水平スケールできない** | server / worker は単一インスタンス前提。レート制限もインメモリ | 利用者が 1 名 + 友人数名。スケールが必要になる負荷が発生しない |
| **依存方向を lint で強制していない** | レイヤー違反はレビューでしか検出できない | 現時点で違反 0 件を維持できている。違反が出始めたら depguard の導入を検討 |
| **radio が Mac に依存** | Mac がスリープしていると当日は欠番 | VOICEVOX と Ollama を動かす計算資源が Pi にない。欠番は許容範囲(縮退許容の原則) |
| **境界の DTO が repository 型のまま** | repository のスキーマ変更が handler に波及しうる | フロントエンドが 1 つ、API 利用者が 1 人。変換層のコストが利益を上回る |
| **書籍 RAG が別リポジトリ(Python)** | 言語をまたぐ運用が必要 | pgvector・PDF 解析・faster-whisper のエコシステムが Python に集中しているため |
