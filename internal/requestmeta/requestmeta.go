package requestmeta

const (
	HeaderVirtualKey            = "X-Gateyes-Internal-Virtual-Key"
	HeaderResolvedProvider      = "X-Gateyes-Internal-Resolved-Provider"
	HeaderResolvedModel         = "X-Gateyes-Internal-Resolved-Model"
	HeaderUsagePromptTokens     = "X-Gateyes-Internal-Usage-Prompt-Tokens"
	HeaderUsageCompletionTokens = "X-Gateyes-Internal-Usage-Completion-Tokens"
	HeaderUsageTotalTokens      = "X-Gateyes-Internal-Usage-Total-Tokens"
	HeaderUsageEstimatedTokens  = "X-Gateyes-Internal-Usage-Estimated-Tokens"
	HeaderStreamRequest         = "X-Gateyes-Internal-Stream-Request"
	HeaderRetryCount            = "X-Gateyes-Internal-Retry-Count"
	HeaderFallbackCount         = "X-Gateyes-Internal-Fallback-Count"
	HeaderCircuitOpenCount      = "X-Gateyes-Internal-Circuit-Open-Count"
	HeaderCacheStatus           = "X-Gateyes-Internal-Cache-Status"
)
