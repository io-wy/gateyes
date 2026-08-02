package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
)

func (h *AdminHandler) SyncRouter(c *gin.Context) {
	if h.routerSvc == nil {
		writeError(c, http.StatusServiceUnavailable, CodeServiceUnavailable, "router sync is not configured")
		return
	}
	var req config.RouterConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	if req.Strategy == "" {
		req.Strategy = "round_robin"
	}
	cfg := config.DefaultConfig()
	cfg.Router = req
	if err := cfg.Validate(); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, err.Error())
		return
	}
	if err := h.routerSvc.Reload(&config.Config{Router: req}); err != nil {
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "sync.router", "router", "runtime", gin.H{
		"strategy":   req.Strategy,
		"rule_count": len(req.RuleEngine.Rules),
	})
	writeOK(c, gin.H{
		"synced":     true,
		"strategy":   req.Strategy,
		"rule_count": len(req.RuleEngine.Rules),
	})
}

type budgetSyncRequest struct {
	SubjectKind     string    `json:"subject_kind"`
	SubjectName     string    `json:"subject_name"`
	BudgetUSD       float64   `json:"budget_usd"`
	BudgetPolicy    string    `json:"budget_policy"`
	RateLimitQPS    int       `json:"rate_limit_qps"`
	MonthlyTokens   int64     `json:"monthly_tokens"`
	AlertThresholds []float64 `json:"alert_thresholds"`
}

func (h *AdminHandler) SyncBudget(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	var req budgetSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	req.SubjectKind = strings.TrimSpace(req.SubjectKind)
	req.SubjectName = strings.TrimSpace(req.SubjectName)
	if req.SubjectKind == "" || req.SubjectName == "" {
		writeError(c, http.StatusBadRequest, CodeMissingRequiredField, "subject_kind and subject_name are required")
		return
	}
	policy := req.BudgetPolicy
	if strings.TrimSpace(policy) == "" {
		policy = "hard_reject"
	}
	if req.SubjectKind == "tenant" && tenantID != "" && req.SubjectName != tenantID {
		writeError(c, http.StatusForbidden, CodeInsufficientRole, "cannot sync another tenant budget")
		return
	}

	var err error
	switch req.SubjectKind {
	case "tenant":
		_, err = h.store.UpdateTenant(c.Request.Context(), req.SubjectName, repository.UpdateTenantParams{
			BudgetUSD:    &req.BudgetUSD,
			BudgetPolicy: &policy,
		})
	case "project":
		_, err = h.store.UpdateProject(c.Request.Context(), tenantID, req.SubjectName, repository.UpdateProjectParams{
			BudgetUSD:    &req.BudgetUSD,
			BudgetPolicy: &policy,
		})
	case "apiKey":
		_, err = h.store.UpdateAPIKey(c.Request.Context(), tenantID, req.SubjectName, repository.UpdateAPIKeyParams{
			BudgetUSD:    &req.BudgetUSD,
			BudgetPolicy: &policy,
			RateLimitQPS: positiveIntPtr(req.RateLimitQPS),
		})
	case "virtualKey":
		_, err = h.store.UpdateVirtualKey(c.Request.Context(), tenantID, req.SubjectName, repository.UpdateVirtualKeyParams{
			BudgetUSD:    &req.BudgetUSD,
			BudgetPolicy: &policy,
			RateLimitQPS: positiveIntPtr(req.RateLimitQPS),
		})
	default:
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, "unsupported budget subject kind")
		return
	}
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeBadRequest, "budget subject not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "sync.budget", req.SubjectKind, req.SubjectName, req)
	writeOK(c, gin.H{
		"synced":         true,
		"subject_kind":   req.SubjectKind,
		"subject_name":   req.SubjectName,
		"budget_usd":     req.BudgetUSD,
		"budget_policy":  policy,
		"rate_limit_qps": req.RateLimitQPS,
	})
}

func positiveIntPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
