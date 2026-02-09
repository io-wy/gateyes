package middleware

import "net/http"

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(payload []byte) (int, error) {
	count, err := r.ResponseWriter.Write(payload)
	r.bytes += count
	return count, err
}
