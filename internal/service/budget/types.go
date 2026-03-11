package budget

import (
	"net"
	"net/http"
	"strings"
	"time"

	"gateyes/internal/config"
)

type Subject struct {
	User       string
	VirtualKey string
	Tenant     string
	Model      string
	Provider   string
	IP         string
	Path       string
}

type quotaRule struct {
	name             string
	dimensions       []string
	tenantHeader     string
	requestsPerDay   int64
	requestsPerMonth int64
	tokensPerDay     int64
	tokensPerMonth   int64
	legacyRequests   int64
	legacyWindow     time.Duration
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func resolveSubject(
	req *http.Request,
	authCfg config.AuthConfig,
	tenantHeader string,
	fallbackModel string,
) Subject {
	token := strings.TrimSpace(extractToken(req, authCfg.Header, authCfg.QueryParam))
	virtualKey := ""
	if token != "" {
		if virtualCfg, ok := authCfg.VirtualKeys[token]; ok && virtualCfg.Enabled {
			virtualKey = token
		}
	}

	tenantValue := strings.TrimSpace(req.Header.Get(tenantHeader))
	if tenantValue == "" {
		tenantValue = strings.TrimSpace(req.Header.Get("X-Tenant-ID"))
	}

	model := strings.TrimSpace(fallbackModel)
	provider := strings.TrimSpace(req.Header.Get("X-Gateyes-Provider"))
	if provider == "" {
		provider = strings.TrimSpace(req.URL.Query().Get("provider"))
	}

	return Subject{
		User:       token,
		VirtualKey: virtualKey,
		Tenant:     tenantValue,
		Model:      model,
		Provider:   provider,
		IP:         normalizeRemoteAddr(req.RemoteAddr),
		Path:       req.URL.Path,
	}
}

func extractToken(req *http.Request, header, queryParam string) string {
	if header != "" {
		value := req.Header.Get(header)
		if value != "" {
			lower := strings.ToLower(value)
			if strings.HasPrefix(lower, "bearer ") {
				return strings.TrimSpace(value[7:])
			}
			return value
		}
	}
	if queryParam != "" {
		return req.URL.Query().Get(queryParam)
	}
	return ""
}

func normalizeDimensionList(dimensions []string) []string {
	if len(dimensions) == 0 {
		return []string{"virtual_key"}
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(dimensions))
	for _, dim := range dimensions {
		value := strings.ToLower(strings.TrimSpace(dim))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return []string{"virtual_key"}
	}
	return out
}

func buildDimensionKey(dimensions []string, subject Subject) string {
	parts := make([]string, 0, len(dimensions))
	for _, dim := range normalizeDimensionList(dimensions) {
		parts = append(parts, dim+"="+sanitizeDimensionValue(dimensionValue(dim, subject)))
	}
	return strings.Join(parts, "|")
}

func dimensionValue(dim string, subject Subject) string {
	switch dim {
	case "user":
		return subject.User
	case "virtual_key":
		return subject.VirtualKey
	case "tenant":
		return subject.Tenant
	case "model":
		return subject.Model
	case "provider":
		return subject.Provider
	case "ip":
		return subject.IP
	case "path":
		return subject.Path
	default:
		return ""
	}
}

func sanitizeDimensionValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	trimmed = strings.ReplaceAll(trimmed, "|", "_")
	trimmed = strings.ReplaceAll(trimmed, "=", "_")
	return trimmed
}

func normalizeRemoteAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func legacyDimension(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ip":
		return "ip"
	case "header":
		return "tenant"
	case "virtual_key":
		return "virtual_key"
	case "auth":
		fallthrough
	default:
		return "virtual_key"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func withDefaultName(name, fallback string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return fallback
}

func dayBucket(now time.Time) (string, time.Duration) {
	date := now.Format("20060102")
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return date, next.Sub(now) + time.Minute
}

func monthBucket(now time.Time) (string, time.Duration) {
	date := now.Format("200601")
	next := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	return date, next.Sub(now) + time.Minute
}
