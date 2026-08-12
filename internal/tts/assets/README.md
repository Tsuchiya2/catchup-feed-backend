# 番組ジングル素材

`opening.mp3` / `ending.mp3` は各エピソード mp3 の先頭・末尾に結合される番組ジングル(D-36)。

| ファイル | 尺 | 出自 |
|---|---|---|
| `opening.mp3` | 10.0 秒 | Stable Audio (Stability AI) で生成 |
| `ending.mp3` | 12.0 秒 | Stable Audio (Stability AI) で生成 |

## クレジット表記

**不要**。第三者の権利者に対する表記義務が発生しないため、U-13(VOICEVOX クレジット)のようなショーノートへの自動挿入は行わない。素材を第三者提供のものに差し替える場合は、表記義務の有無を decisions.md で先に決めること。

## 差し替え手順

`//go:embed`(`internal/tts/jingle.go`)でバイナリに同梱しているため、実行時の設定変更では差し替わらない。`internal/feed/assets/artwork.jpg`(D-33)と同じ運用:

1. このディレクトリのファイルを置換する
2. Mac で `cmd/radio` を再ビルドする(`deploy/mac.md` §5: `go build -o ~/pulse/bin/radio ./cmd/radio`)
3. **テストの期待尺を新しい素材に合わせて更新する**(下記)

フォーマット(サンプルレート・チャンネル数)は問わない。実行時にその run の VOICEVOX 出力から実測したフォーマットへデコードされる(D-36 (2))。ただし**尺は `episodes.duration_sec` と §7.1 の18分ガードに算入される**ので、大きく変える場合は影響を確認すること。

### 手順3の詳細(尺をハードコードしている箇所)

尺は2箇所にハードコードされており、**壊れ方が非対称**なので両方を必ず更新すること。

| 箇所 | 内容 | 差し替え後の挙動 |
|---|---|---|
| `internal/tts/jingle_test.go` の `TestFFmpeg_DecodeJingles_RealFFmpeg` | 実 ffmpeg でデコードした実測尺を 10s / 12s(±0.5)と照合 | **落ちる。ただし ffmpeg のある Mac でだけ**。CI には ffmpeg が無く `t.Skip` するので気づけない |
| `internal/radio/pipeline_test.go` の `fakeOpeningJingle` / `fakeEndingJingle` / `jingleSec` | パイプラインのテストが使う偽の尺 | **落ちない。実態とズレたまま通り続ける**。`duration_sec` や18分ガードの検証が実物と無関係な値で行われる |

つまり CI だけ見ていると差し替えに気づけない。素材を替えたら `ffprobe -v error -show_entries format=duration -of default=nw=1 opening.mp3` で実尺を確認し、上表の両方を手で合わせること(`jingleSec` は opening + ending の秒数)。

`loudnorm` は結合後の全体に1パスかかる。音楽と音声で音量差が大きいと遷移でゲインが動くため、差し替え時は最初の実エピソードを聴いて確認すること。
