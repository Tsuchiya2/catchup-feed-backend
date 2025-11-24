// Package respond provides utilities for sending HTTP responses in JSON format.
// It includes error handling with sanitization to prevent leaking sensitive information.
package respond

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

// JSON writes a JSON response with the given status code and data.
func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			// Log the error but cannot send error response as headers already sent
			slog.Default().Error("failed to encode JSON response",
				slog.Int("status_code", code),
				slog.Any("error", err))
		}
	}
}

// Error writes a JSON error response with the given status code and error message.
func Error(w http.ResponseWriter, code int, err error) {
	JSON(w, code, map[string]string{"error": err.Error()})
}

// SafeError sanitizes error messages before returning them to users.
// Internal errors (e.g., database errors) are returned as "internal server error",
// with details logged for debugging. Safe errors (validation errors) are returned as-is.
func SafeError(w http.ResponseWriter, code int, err error) {
	if err == nil {
		return
	}

	// ユーザーに安全に返せるエラーかどうかを判定
	msg := err.Error()

	// バリデーションエラーなど、ユーザーに返してOKなエラー
	safeErrors := []string{
		"required",
		"invalid",
		"not found",
		"already exists",
		"must be",
		"cannot be",
		"too long",
		"too short",
	}

	isSafe := false
	lowerMsg := strings.ToLower(msg)
	for _, safe := range safeErrors {
		if strings.Contains(lowerMsg, safe) {
			isSafe = true
			break
		}
	}

	// 500エラーは常に内部エラーとして扱う
	if code >= 500 {
		isSafe = false
	}

	if isSafe {
		// 安全なエラーはそのまま返す
		JSON(w, code, map[string]string{"error": msg})
	} else {
		// 内部エラーはログに出力し、汎用メッセージを返す
		// 機密情報をマスクしてログ出力
		logger := slog.Default()
		logger.Error("internal server error",
			slog.String("status", http.StatusText(code)),
			slog.Int("code", code),
			slog.Any("error", SanitizeError(err)))
		JSON(w, code, map[string]string{"error": "internal server error"})
	}
}

// AppError is an error type that carries a user-facing message.
type AppError struct {
	UserMsg string // Message to display to users
	Err     error  // Internal error (logged for debugging)
	Code    int    // HTTP status code
}

// Error returns the error message, implementing the error interface.
func (e *AppError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.UserMsg
}

// Unwrap returns the underlying error, implementing the errors.Unwrap interface.
func (e *AppError) Unwrap() error {
	return e.Err
}

// NewAppError creates a new AppError with the given parameters.
func NewAppError(code int, userMsg string, err error) *AppError {
	return &AppError{Code: code, UserMsg: userMsg, Err: err}
}

// SafeErrorV2 handles errors with AppError support.
// If the error is an AppError, it returns the user message and logs the internal error.
// Otherwise, it falls back to SafeError behavior.
func SafeErrorV2(w http.ResponseWriter, code int, err error) {
	if err == nil {
		return
	}

	var appErr *AppError
	if errors.As(err, &appErr) {
		// AppErrorの場合、ユーザー向けメッセージを返す
		if appErr.Err != nil {
			// 機密情報をマスクしてログ出力
			logger := slog.Default()
			logger.Error("application error",
				slog.String("status", http.StatusText(appErr.Code)),
				slog.Int("code", appErr.Code),
				slog.String("user_message", appErr.UserMsg),
				slog.Any("error", SanitizeError(appErr.Err)))
		}
		JSON(w, appErr.Code, map[string]string{"error": appErr.UserMsg})
		return
	}

	// AppErrorでない場合、SafeErrorの処理にフォールバック
	SafeError(w, code, err)
}
