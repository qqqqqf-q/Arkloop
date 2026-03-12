package httputil

import "net/http"

// StatusRecorder wraps http.ResponseWriter and captures the status code.
type StatusRecorder struct {
	http.ResponseWriter
	StatusCode  int
	WroteHeader bool
}

func (r *StatusRecorder) WriteHeader(statusCode int) {
	if r.WroteHeader {
		return
	}
	r.WroteHeader = true
	r.StatusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *StatusRecorder) Write(payload []byte) (int, error) {
	if !r.WroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(payload)
}

func (r *StatusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
