package ingress

import (
	"strconv"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/proxy"
)

const (
	annotPrefix = "nginx.ingress.kubernetes.io/"
)

// ParseAnnotations translates nginx ingress annotations to internal config.
func ParseAnnotations(raw map[string]string) *proxy.Annotations {
	a := &proxy.Annotations{Raw: raw}

	for k, v := range raw {
		key := strings.TrimPrefix(k, annotPrefix)
		switch key {
		case "rewrite-target":
			a.RewriteTarget = v
		case "ssl-redirect":
			a.SSLRedirect = parseBool(v)
		case "force-ssl-redirect":
			a.SSLRedirect = parseBool(v)
		case "proxy-body-size":
			a.ProxyBodySize = parseByteSize(v)
		case "proxy-read-timeout":
			a.ProxyReadTimeout = parseDurationSeconds(v)
		case "proxy-send-timeout":
			a.ProxySendTimeout = parseDurationSeconds(v)
		case "proxy-connect-timeout":
			a.ProxyConnectTimeout = parseDurationSeconds(v)
		case "enable-cors":
			a.EnableCORS = parseBool(v)
		case "cors-allow-origin":
			a.CORSAllowOrigin = splitComma(v)
		case "cors-allow-methods":
			a.CORSAllowMethods = splitComma(v)
		case "cors-allow-headers":
			a.CORSAllowHeaders = splitComma(v)
		case "cors-allow-credentials":
			a.CORSAllowCredentials = parseBool(v)
		case "limit-rps":
			a.RateLimitRPS = parseFloat(v)
		case "limit-connections":
			a.RateLimitConnections = parseInt(v)
		case "affinity":
			a.Affinity = v
		case "session-cookie-name":
			a.AffinityCookieName = v
		case "canary":
			a.Canary = parseBool(v)
		case "canary-weight":
			a.CanaryWeight = parseInt(v)
		case "canary-by-header":
			a.CanaryByHeader = v
		case "whitelist-source-range":
			a.WhitelistSourceRange = splitComma(v)
		case "denylist-source-range":
			// Map denylist to empty whitelist for MVP; full ACL later.
		case "backend-protocol":
			a.BackendProtocol = strings.ToUpper(v)
		case "proxy-next-upstream":
			a.ProxyNextUpstream = parseBool(v)
		case "proxy-next-upstream-tries":
			a.ProxyNextUpstreamTries = parseInt(v)
		case "enable-gzip":
			a.EnableGzip = parseBool(v)
		}
	}
	return a
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "yes" || s == "1" || s == "on"
}

func parseInt(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func parseDurationSeconds(s string) time.Duration {
	v := parseInt(s)
	if v <= 0 {
		return 0
	}
	return time.Duration(v) * time.Second
}

func parseByteSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "Gi") || strings.HasSuffix(s, "gi"):
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "Mi") || strings.HasSuffix(s, "mi"):
		multiplier = 1024 * 1024
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "Ki") || strings.HasSuffix(s, "ki"):
		multiplier = 1024
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "G") || strings.HasSuffix(s, "g"):
		multiplier = 1000 * 1000 * 1000
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "M") || strings.HasSuffix(s, "m"):
		multiplier = 1000 * 1000
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "K") || strings.HasSuffix(s, "k"):
		multiplier = 1000
		s = s[:len(s)-1]
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v * multiplier
}

func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
