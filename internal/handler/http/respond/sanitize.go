package respond

import (
	"regexp"
)

var (
	// API キーパターン
	// 注意: anthropicKeyPatternを先に適用する（より具体的なパターンから）
	anthropicKeyPattern = regexp.MustCompile(`sk-ant-[a-zA-Z0-9-_]+`)
	// OpenAIのパターンは、既にマスクされた文字列（*を含む）にマッチしないようにする
	openaiKeyPattern = regexp.MustCompile(`sk-[a-zA-Z0-9]{10,}`)

	// データベースパスワードパターン（DSN内）
	dbPasswordPattern = regexp.MustCompile(`://([^:]+):([^@]+)@`)
)

// SanitizeError は機密情報をマスクしたエラーメッセージを返す
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()

	// APIキーのマスク（順序重要: より具体的なパターンから適用）
	msg = anthropicKeyPattern.ReplaceAllString(msg, "sk-ant-****")
	msg = openaiKeyPattern.ReplaceAllString(msg, "sk-****")

	// DBパスワードのマスク
	msg = dbPasswordPattern.ReplaceAllString(msg, "://$1:****@")

	return msg
}
