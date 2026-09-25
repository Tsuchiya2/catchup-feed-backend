//go:build swagdep

// Package swagdep は github.com/swaggo/swag を「直接依存」として go.mod に
// 固定するためだけに存在する。ビルドタグ `swagdep` はどこでも設定しないので、
// このファイルがバイナリに入ることはない。
//
// 背景: swag を実際に import しているのは swag 自身の生成物 docs/docs.go
// (`import "github.com/swaggo/swag"`)だが、docs/docs.go は .gitignore 済みで
// コミットされない。そのため go.mod の正解がツリーの状態に依存してしまう:
//
//	Swagger 生成済み(開発者・CI)   -> go mod tidy は direct と判定
//	Swagger 未生成(dependabot)     -> go mod tidy は // indirect に降格
//
// 結果として dependabot の gomod PR が降格を持ち込み、生成済みの正規状態で
// tidy した全員に差分が出続けた(#122・#124・#130 で3回発生)。
//
// go.mod の `tool github.com/swaggo/swag/cmd/swag` はこれを防げない。Go の
// // indirect は「main module のパッケージがそのモジュールを import していない」
// ことを表すマーカーであり、tool ディレクティブはその判定に参加しないため
// (`go get -tool` 単体でも // indirect が付く)。
//
// 疑ったら使い捨てのツリーで再現できる(所要30秒)。docs/docs.go とこのファイル
// の両方を消して tidy すると、tool ディレクティブが go.mod に残ったままでも
// swag が // indirect に落ちる:
//
//	rm docs/docs.go swagdep.go && go mod tidy
//	grep -n 'swaggo/swag\|^tool ' go.mod   # tool 行は残るが swag は // indirect
//
// このファイルだけ戻して(docs/docs.go は消したまま)もう一度 tidy すると
// direct に戻る。それが swagdep.go の効き目そのもの。確認後は
// `git restore .` と `make swagger-host` で元に戻すこと。
//
// このファイルがあると tidy の結果が生成の有無に依存しなくなり、降格が構造的に
// 起きなくなる。**このファイルを消さないこと。** 消すと降格が再発する。
//
// go.mod 側の3者は役割が別々なので混同しないこと:
//
//	require github.com/swaggo/swag v1.16.6  モジュールの**バージョンを選ぶ**のはここ
//	tool github.com/swaggo/swag/cmd/swag    `go tool swag` で呼ぶツールの**パッケージ
//	                                        パスを名指す**だけ。バージョンは持たない
//	この swagdep.go                         swag を **direct 依存として require に
//	                                        居続けさせる**だけ。バージョンは持たない
//
// したがってバージョンを上げる操作は従来どおり `go get github.com/swaggo/swag@vX.Y.Z`
// の1回で、全経路(CI / Docker / make swagger / make swagger-host)に反映される。
package swagdep

import _ "github.com/swaggo/swag"
