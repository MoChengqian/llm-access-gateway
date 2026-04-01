package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type contextKey string

const requestIDKey contextKey = "request_id"

var requestCounter atomic.Uint64

type WrapResponseWriter interface {
	http.ResponseWriter
	Status() int
	BytesWritten() int
}

type wrapResponseWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = fmt.Sprintf("%d-%d", time.Now().UnixNano(), requestCounter.Add(1))
		}

		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RealIP(next http.Handler) http.Handler {
	return next
}

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func GetReqID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey).(string)
	return requestID
}

func NewWrapResponseWriter(w http.ResponseWriter, _ int) WrapResponseWriter {
	return &wrapResponseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
	}
}

func (w *wrapResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *wrapResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	written, err := w.ResponseWriter.Write(b)
	w.bytesWritten += written
	return written, err
}

func (w *wrapResponseWriter) Flush() {
	flusher, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}

	flusher.Flush()
}

func (w *wrapResponseWriter) Status() int {
	return w.status
}

func (w *wrapResponseWriter) BytesWritten() int {
	return w.bytesWritten
}
