package responses

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

const defaultCCHValue = "A1234"

var (
	cchRewritePattern = regexp.MustCompile(`(?s)^(\s*x-anthropic-billing-header:.*?\bcch=)[^;\s]+(;?)`)
	claudeDatePattern = regexp.MustCompile(`(# currentDate\r?\n)Today(?:'|\x{2019}|\x{02BC}|\x{02B9})s date is ([0-9]{4})[/-]([0-9]{2})[/-]([0-9]{2})\.(\r?\n)`)
)

func (s *Service) cachePromptRewriteEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Cache.PromptRewrite
}

func (s *Service) applyCachePromptRewrite(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest) *provider.ResponseRequest {
	if req == nil || !s.cachePromptRewriteEnabled() {
		return req
	}

	next := buildUpstreamRequest(req)
	if next.Options != nil && next.Options.System != "" {
		rewritten, rewrites := normalizePromptSystem(next.Options.System)
		if rewritten != next.Options.System {
			next.Options.System = rewritten
			for _, rewrite := range rewrites {
				appendCacheRewrite(ctx, rewrite)
			}
		}
	}

	if strings.TrimSpace(next.PromptCacheKey) == "" && shouldInjectPromptCacheKey(next) {
		next.PromptCacheKey = buildPromptCacheKey(identity, next)
		appendCacheRewrite(ctx, "prompt_cache_key")
	}
	if strings.TrimSpace(next.PromptCacheKey) != "" {
		setPromptCacheKeyTrace(ctx, next.PromptCacheKey)
	}

	return next
}

func normalizePromptSystem(system string) (string, []string) {
	rewritten := system
	var rewrites []string

	if strings.HasPrefix(strings.TrimSpace(rewritten), "x-anthropic-billing-header:") {
		next := cchRewritePattern.ReplaceAllString(rewritten, "${1}"+defaultCCHValue+"${2}")
		if next != rewritten {
			rewritten = next
			rewrites = append(rewrites, "anthropic_cch")
		}
	}

	next := claudeDatePattern.ReplaceAllString(rewritten, "${1}Today's date is ${2}-${3}-${4}.${5}")
	if next != rewritten {
		rewritten = next
		rewrites = append(rewrites, "claude_code_date")
	}

	return rewritten, rewrites
}

func shouldInjectPromptCacheKey(req *provider.ResponseRequest) bool {
	if req == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(req.Surface)) {
	case "responses", "chat":
		return true
	default:
		return false
	}
}

func buildPromptCacheKey(identity *repository.AuthIdentity, req *provider.ResponseRequest) string {
	hostParts := make([]string, 0, 2)
	if identity != nil {
		if value := strings.TrimSpace(identity.TenantID); value != "" {
			hostParts = append(hostParts, value)
		}
		if value := strings.TrimSpace(identity.ProjectID); value != "" {
			hostParts = append(hostParts, value)
		}
	}
	hostKey := "local"
	if len(hostParts) > 0 {
		sum := sha256.Sum256([]byte(strings.Join(hostParts, ":")))
		hostKey = hex.EncodeToString(sum[:])[:12]
	}

	clientName := "unknown"
	if req != nil {
		if value := strings.TrimSpace(req.Surface); value != "" {
			clientName = value
		}
	}
	return hostKey + ":" + clientName
}
