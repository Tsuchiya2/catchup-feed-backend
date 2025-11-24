// Package responsewriter provides a wrapper for http.ResponseWriter that records response metrics.
// It tracks status codes and bytes written for logging and monitoring purposes.
package responsewriter

import (
	"net/http"
)

// ResponseWriter wraps http.ResponseWriter to record response metrics.
type ResponseWriter struct {
	http.ResponseWriter
	statusCode    int
	bytesWritten  int
	headerWritten bool
}

// Wrap wraps an http.ResponseWriter and returns a new ResponseWriter for metric recording.
func Wrap(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default is 200
	}
}

// WriteHeader records the status code and calls the underlying WriteHeader.
func (w *ResponseWriter) WriteHeader(statusCode int) {
	if !w.headerWritten {
		w.statusCode = statusCode
		w.headerWritten = true
		w.ResponseWriter.WriteHeader(statusCode)
	}
}

// Write writes the response body and records the size.
func (w *ResponseWriter) Write(b []byte) (int, error) {
	if !w.headerWritten {
		// Implicitly write 200 if WriteHeader hasn't been called
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += n
	return n, err
}

// StatusCode returns the recorded HTTP status code.
func (w *ResponseWriter) StatusCode() int {
	return w.statusCode
}

// BytesWritten returns the number of bytes written to the response.
func (w *ResponseWriter) BytesWritten() int {
	return w.bytesWritten
}

// Unwrap returns the underlying http.ResponseWriter (for http.ResponseController support).
func (w *ResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
