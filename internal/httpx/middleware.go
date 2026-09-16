package httpx

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/uvwt/agentdock/internal/requestid"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n
	return n, err
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := requestid.Normalize(r.Header.Get(requestid.Header))
		if id == "" {
			id = requestid.New()
		}
		r = r.WithContext(requestid.With(r.Context(), id))
		w.Header().Set(requestid.Header, id)
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		// 只记录元数据，不记录 header/body，避免把 bearer token、OAuth code、工具参数写进日志。
		slog.Info("http request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", status, "bytes", recorder.bytes, "duration_ms", time.Since(started).Milliseconds(), "remote", r.RemoteAddr)
	})
}
