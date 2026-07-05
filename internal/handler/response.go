package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

type Code int

const (
	CodeOK Code = 20000

	// 400xx 认证与鉴权
	CodeInvalidAPIKey     Code = 40001
	CodeInactiveAPIKey    Code = 40002
	CodeInvalidSecret     Code = 40003
	CodeInvalidVirtualKey Code = 40004
	CodeExpiredVirtualKey Code = 40005
	CodeInvalidToken      Code = 40006

	// 401xx 权限与授权
	CodeInsufficientRole   Code = 40101
	CodeModelNotAllowed    Code = 40102
	CodeProviderNotAllowed Code = 40103
	CodeServiceNotAllowed  Code = 40104
	CodeSurfaceNotAllowed  Code = 40105

	// 404xx 资源不存在
	CodeProviderNotFound   Code = 40401
	CodeServiceNotFound    Code = 40402
	CodeUserNotFound       Code = 40403
	CodeProjectNotFound    Code = 40404
	CodeAPIKeyNotFound     Code = 40405
	CodeVirtualKeyNotFound Code = 40406
	CodeResponseNotFound   Code = 40407
	CodePluginNotFound     Code = 40408

	// 405xx 业务状态异常
	CodeServiceNotPublished   Code = 40501
	CodeServiceDisabled       Code = 40502
	CodeServiceAccessDenied   Code = 40503
	CodePromptTemplateMissing Code = 40504
	CodePolicyViolation       Code = 40505
	CodeNoAvailableProvider   Code = 40506

	// 406xx 配额与预算
	CodeQuotaExceeded  Code = 40601
	CodeBudgetExceeded Code = 40602

	// 407xx 限流
	CodeRateLimited Code = 40701

	// 408xx 请求校验
	CodeBadRequest           Code = 40800
	CodeInvalidRequestBody   Code = 40801
	CodeMissingRequiredField Code = 40802
	CodeInvalidParameter     Code = 40803

	// 500xx 服务端内部错误
	CodeInternalError Code = 50001
	CodeDatabaseError Code = 50002

	// 502xx 上游错误
	CodeUpstreamError       Code = 50201
	CodeUpstreamTimeout     Code = 50202
	CodeUpstreamUnavailable Code = 50203

	// 503xx 服务不可用
	CodeServiceUnavailable Code = 50301
	CodeNoHealthyProvider  Code = 50302
	CodeConfigReloadFailed Code = 50303
)

var codeMessages = map[Code]string{
	CodeOK:                    "ok",
	CodeBadRequest:            "bad request",
	CodeInvalidAPIKey:         "invalid API key",
	CodeInactiveAPIKey:        "inactive API key",
	CodeInvalidSecret:         "invalid secret",
	CodeInvalidVirtualKey:     "invalid virtual key",
	CodeExpiredVirtualKey:     "expired virtual key",
	CodeInsufficientRole:      "insufficient role",
	CodeModelNotAllowed:       "model not allowed",
	CodeProviderNotAllowed:    "provider not allowed",
	CodeServiceNotAllowed:     "service not allowed",
	CodeSurfaceNotAllowed:     "surface not allowed",
	CodeProviderNotFound:      "provider not found",
	CodeServiceNotFound:       "service not found",
	CodeUserNotFound:          "user not found",
	CodeProjectNotFound:       "project not found",
	CodeAPIKeyNotFound:        "API key not found",
	CodeVirtualKeyNotFound:    "virtual key not found",
	CodeResponseNotFound:      "response not found",
	CodeServiceNotPublished:   "service not published",
	CodeServiceDisabled:       "service disabled",
	CodeServiceAccessDenied:   "service access denied",
	CodePromptTemplateMissing: "prompt template not configured",
	CodePolicyViolation:       "policy violation",
	CodeNoAvailableProvider:   "no available provider",
	CodeQuotaExceeded:         "quota exceeded",
	CodeBudgetExceeded:        "budget exceeded",
	CodeRateLimited:           "rate limit exceeded",
	CodeInvalidRequestBody:    "invalid request body",
	CodeMissingRequiredField:  "missing required field",
	CodeInvalidParameter:      "invalid parameter",
	CodeInternalError:         "internal server error",
	CodeDatabaseError:         "database error",
	CodeUpstreamError:         "upstream provider error",
	CodeUpstreamTimeout:       "upstream timeout",
	CodeUpstreamUnavailable:   "upstream unavailable",
	CodeServiceUnavailable:    "service unavailable",
	CodeNoHealthyProvider:     "no healthy provider",
	CodeConfigReloadFailed:    "config reload failed",
}

func writeJSON(c *gin.Context, httpStatus int, code Code, msg string, data any) {
	success := code == CodeOK
	if msg == "" {
		msg = codeMessages[code]
		if msg == "" {
			msg = "unknown error"
		}
	}
	resp := gin.H{
		"code":    code,
		"success": success,
		"message": msg,
	}
	if data != nil {
		switch v := data.(type) {
		case gin.H:
			for k, val := range v {
				resp[k] = val
			}
		case map[string]any:
			for k, val := range v {
				resp[k] = val
			}
		case ListData:
			resp["items"] = v.Items
			resp["total"] = v.Total
		default:
			resp["data"] = v
		}
	}
	// Embed OpenAI-compatible error field for progressive migration.
	if !success {
		resp["error"] = gin.H{
			"message": msg,
			"type":    codeToErrorType(code),
		}
	}
	c.JSON(httpStatus, resp)
}

type ListData struct {
	Items any   `json:"items"`
	Total int64 `json:"total"`
}

func writeOK(c *gin.Context, data any) {
	writeJSON(c, http.StatusOK, CodeOK, "", data)
}

func writeOKMsg(c *gin.Context, msg string, data any) {
	writeJSON(c, http.StatusOK, CodeOK, msg, data)
}

func writeList(c *gin.Context, items any, total int64) {
	writeOK(c, ListData{Items: items, Total: total})
}

func writeError(c *gin.Context, httpStatus int, code Code, msg string) {
	writeJSON(c, httpStatus, code, msg, nil)
}

func writeErrorData(c *gin.Context, httpStatus int, code Code, msg string, data any) {
	writeJSON(c, httpStatus, code, msg, data)
}

func errToCode(err error) Code {
	switch {
	case errors.Is(err, catalog.ErrServiceNotPublished):
		return CodeServiceNotPublished
	case errors.Is(err, catalog.ErrServiceDisabled):
		return CodeServiceDisabled
	case errors.Is(err, catalog.ErrServiceAccessDenied), errors.Is(err, catalog.ErrServiceSurfaceDenied):
		return CodeServiceAccessDenied
	case errors.Is(err, catalog.ErrRateLimited):
		return CodeRateLimited
	case errors.Is(err, catalog.ErrPromptTemplateMissing):
		return CodePromptTemplateMissing
	case errors.Is(err, catalog.ErrPromptVariableMissing), errors.Is(err, catalog.ErrPolicyViolation):
		return CodeBadRequest
	case errors.Is(err, responseSvc.ErrNoProvider):
		return CodeNoAvailableProvider
	default:
		// Prefer structured UpstreamError over string matching.
		var ue *provider.UpstreamError
		if errors.As(err, &ue) {
			if ue.IsTimeout() {
				return CodeUpstreamTimeout
			}
			if ue.IsRateLimited() {
				return CodeRateLimited
			}
			if ue.StatusCode >= http.StatusBadRequest && ue.StatusCode < http.StatusInternalServerError {
				return CodeInvalidAPIKey
			}
			return CodeUpstreamError
		}
		// Fallback: legacy string-matching for plain errors.
		msg := err.Error()
		if strings.Contains(msg, "timeout") {
			return CodeUpstreamTimeout
		}
		if strings.Contains(msg, "401") || strings.Contains(msg, "authentication") {
			return CodeInvalidAPIKey
		}
		if strings.Contains(msg, "403") || strings.Contains(msg, "forbidden") {
			return CodeInvalidAPIKey
		}
		if strings.Contains(msg, "429") || strings.Contains(msg, "rate_limit") {
			return CodeRateLimited
		}
		if strings.Contains(msg, "400") || strings.Contains(msg, "invalid") {
			return CodeBadRequest
		}
		return CodeUpstreamError
	}
}

// codeToErrorType maps internal Code to OpenAI-compatible error type strings.
func codeToErrorType(code Code) string {
	switch {
	case code >= 40001 && code < 40100:
		return "authentication_error"
	case code >= 40101 && code < 40400:
		return "invalid_request_error"
	case code >= 40601 && code < 40700:
		return "rate_limit_error"
	case code >= 50001 && code < 50200:
		return "server_error"
	case code >= 50201 && code < 50400:
		return "server_error"
	default:
		return "invalid_request_error"
	}
}
