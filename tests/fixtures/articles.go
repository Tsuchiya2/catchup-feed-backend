// Package fixtures provides reusable test data generators for integration tests.
// This package eliminates test data duplication and ensures consistent test content
// across different test suites.
package fixtures

import (
	"strings"
)

// ArticleOptions configures the generated article content.
type ArticleOptions struct {
	// Length is the approximate character count (target length, ±10% variance allowed)
	Length int

	// Language specifies the content language ("japanese" or "english")
	Language string

	// IncludeEmoji specifies whether to include emoji characters in the content
	IncludeEmoji bool
}

// GenerateArticle generates article content based on the provided options.
// The generated content is coherent Japanese or English text suitable for summarization testing.
//
// Example:
//
//	article := GenerateArticle(ArticleOptions{
//	    Length: 2000,
//	    Language: "japanese",
//	    IncludeEmoji: false,
//	})
func GenerateArticle(opts ArticleOptions) string {
	if opts.Language == "english" {
		return generateEnglishArticle(opts.Length, opts.IncludeEmoji)
	}
	return generateJapaneseArticle(opts.Length, opts.IncludeEmoji)
}

// GenerateShortArticle generates a short article (~500 characters).
// This is useful for testing summarization of brief content.
//
// Example:
//
//	article := GenerateShortArticle()
//	// Returns Japanese article with approximately 500 characters
func GenerateShortArticle() string {
	return GenerateArticle(ArticleOptions{
		Length:       500,
		Language:     "japanese",
		IncludeEmoji: false,
	})
}

// GenerateMediumArticle generates a medium-length article (~2000 characters).
// This is useful for testing typical article summarization scenarios.
//
// Example:
//
//	article := GenerateMediumArticle()
//	// Returns Japanese article with approximately 2000 characters
func GenerateMediumArticle() string {
	return GenerateArticle(ArticleOptions{
		Length:       2000,
		Language:     "japanese",
		IncludeEmoji: false,
	})
}

// GenerateLongArticle generates a long article (~10000 characters).
// This is useful for testing summarization of extensive content.
//
// Example:
//
//	article := GenerateLongArticle()
//	// Returns Japanese article with approximately 10000 characters
func GenerateLongArticle() string {
	return GenerateArticle(ArticleOptions{
		Length:       10000,
		Language:     "japanese",
		IncludeEmoji: false,
	})
}

// GenerateArticleWithEmoji generates an article that includes emoji characters.
// This is useful for testing Unicode character counting and handling.
//
// Example:
//
//	article := GenerateArticleWithEmoji()
//	// Returns Japanese article with emoji characters
func GenerateArticleWithEmoji() string {
	return GenerateArticle(ArticleOptions{
		Length:       2000,
		Language:     "japanese",
		IncludeEmoji: true,
	})
}

// generateJapaneseArticle generates coherent Japanese article content.
func generateJapaneseArticle(targetLength int, includeEmoji bool) string {
	// Base sentences for Japanese content
	baseSentences := []string{
		"人工知能技術の発展により、私たちの生活は大きく変化しています。",
		"機械学習アルゴリズムは、大量のデータから複雑なパターンを学習することができます。",
		"深層学習モデルは、画像認識や自然言語処理などの分野で優れた性能を発揮しています。",
		"ニューラルネットワークは、人間の脳の構造にヒントを得た計算モデルです。",
		"データサイエンスは、統計学、プログラミング、ドメイン知識を組み合わせた学際的な分野です。",
		"クラウドコンピューティングの普及により、大規模な計算資源を容易に利用できるようになりました。",
		"自然言語処理技術は、テキストの分類、感情分析、機械翻訳などに応用されています。",
		"コンピュータビジョンの進歩により、画像や動画の自動認識が可能になりました。",
		"ビッグデータ解析により、ビジネスインサイトを得ることができます。",
		"IoTデバイスの増加により、リアルタイムデータの収集と分析が重要になっています。",
		"エッジコンピューティングは、データ処理をデバイスの近くで行うことで、レイテンシーを削減します。",
		"量子コンピューティングは、従来のコンピュータでは解決困難な問題に取り組む可能性を秘めています。",
		"ブロックチェーン技術は、分散型システムにおける信頼性の確保に貢献しています。",
		"サイバーセキュリティは、デジタル社会において極めて重要な課題です。",
		"5G通信技術の展開により、超高速・低遅延の通信が実現されつつあります。",
	}

	emojiSentences := []string{
		"技術革新は私たちの未来を明るくします 🚀✨",
		"AIの発展により、新しい可能性が広がっています 🤖💡",
		"データドリブンな意思決定が重要です 📊📈",
		"デジタルトランスフォーメーションが加速しています 💻🌐",
		"イノベーションが社会を変革します 🔬🌟",
	}

	var builder strings.Builder
	currentLength := 0
	sentenceIndex := 0
	emojiIndex := 0

	for {
		var sentence string
		if includeEmoji && currentLength%(targetLength/5) < 100 && emojiIndex < len(emojiSentences) {
			sentence = emojiSentences[emojiIndex]
			emojiIndex++
		} else {
			sentence = baseSentences[sentenceIndex%len(baseSentences)]
			sentenceIndex++
		}

		// Calculate the length if we add this sentence
		sentenceLength := len([]rune(sentence))
		if currentLength > 0 {
			sentenceLength++ // Account for space
		}
		potentialLength := currentLength + sentenceLength

		// If we've reached or exceeded the minimum target (90%), check if we should stop
		if currentLength >= int(float64(targetLength)*0.9) {
			// Stop if adding this sentence would exceed 110% of target
			if potentialLength > int(float64(targetLength)*1.1) {
				break
			}
		}

		// Add spacing before sentence (except for the first one)
		if currentLength > 0 {
			builder.WriteString(" ")
		}

		builder.WriteString(sentence)
		currentLength = len([]rune(builder.String()))

		// Stop if we've reached the target
		if currentLength >= targetLength {
			break
		}
	}

	return builder.String()
}

// generateEnglishArticle generates coherent English article content.
func generateEnglishArticle(targetLength int, includeEmoji bool) string {
	baseSentences := []string{
		"Artificial intelligence technology is rapidly transforming our daily lives.",
		"Machine learning algorithms can learn complex patterns from large datasets.",
		"Deep learning models excel in areas such as image recognition and natural language processing.",
		"Neural networks are computational models inspired by the structure of the human brain.",
		"Data science combines statistics, programming, and domain expertise.",
		"Cloud computing has made large-scale computational resources easily accessible.",
		"Natural language processing is applied to text classification, sentiment analysis, and machine translation.",
		"Computer vision advances enable automatic recognition of images and videos.",
		"Big data analytics provides valuable business insights.",
		"The proliferation of IoT devices has made real-time data collection and analysis crucial.",
		"Edge computing reduces latency by processing data closer to the source.",
		"Quantum computing holds promise for solving problems intractable for classical computers.",
		"Blockchain technology contributes to ensuring trust in distributed systems.",
		"Cybersecurity is a critical challenge in the digital age.",
		"5G technology deployment is enabling ultra-fast, low-latency communications.",
	}

	emojiSentences := []string{
		"Technological innovation brightens our future 🚀✨",
		"AI development opens new possibilities 🤖💡",
		"Data-driven decision making is essential 📊📈",
		"Digital transformation is accelerating 💻🌐",
		"Innovation transforms society 🔬🌟",
	}

	var builder strings.Builder
	currentLength := 0
	sentenceIndex := 0
	emojiIndex := 0

	for {
		var sentence string
		if includeEmoji && currentLength%(targetLength/5) < 100 && emojiIndex < len(emojiSentences) {
			sentence = emojiSentences[emojiIndex]
			emojiIndex++
		} else {
			sentence = baseSentences[sentenceIndex%len(baseSentences)]
			sentenceIndex++
		}

		// Calculate the length if we add this sentence
		sentenceLength := len([]rune(sentence))
		if currentLength > 0 {
			sentenceLength++ // Account for space
		}
		potentialLength := currentLength + sentenceLength

		// If we've reached or exceeded the minimum target (90%), check if we should stop
		if currentLength >= int(float64(targetLength)*0.9) {
			// Stop if adding this sentence would exceed 110% of target
			if potentialLength > int(float64(targetLength)*1.1) {
				break
			}
		}

		// Add spacing before sentence (except for the first one)
		if currentLength > 0 {
			builder.WriteString(" ")
		}

		builder.WriteString(sentence)
		currentLength = len([]rune(builder.String()))

		// Stop if we've reached the target
		if currentLength >= targetLength {
			break
		}
	}

	return builder.String()
}
