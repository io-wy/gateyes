package handler

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/service/oidc"
)

func (h *AdminHandler) OIDCStatus(c *gin.Context) {
	mwSvc := middlewareFromContext(c)
	enabled := mwSvc != nil && mwSvc.OIDC() != nil && mwSvc.OIDC().Enabled()
	writeOK(c, gin.H{"enabled": enabled})
}

func (h *AdminHandler) OIDCLogin(c *gin.Context) {
	mwSvc := middlewareFromContext(c)
	if mwSvc == nil || mwSvc.OIDC() == nil || !mwSvc.OIDC().Enabled() {
		writeError(c, http.StatusNotImplemented, CodeServiceUnavailable, "OIDC not configured")
		return
	}

	oidcSvc := mwSvc.OIDC()
	pkce, err := oidc.GeneratePKCE()
	if err != nil {
		writeInternalError(c, err)
		return
	}
	state, err := oidc.GenerateState()
	if err != nil {
		writeInternalError(c, err)
		return
	}
	nonce, err := oidc.GenerateNonce()
	if err != nil {
		writeInternalError(c, err)
		return
	}

	// Persist state/nonce/verifier in cookie for callback verification.
	c.SetCookie("oidc_state", state, 600, "/", "", false, true)
	c.SetCookie("oidc_nonce", nonce, 600, "/", "", false, true)
	c.SetCookie("oidc_verifier", pkce.Verifier, 600, "/", "", false, true)

	writeOK(c, gin.H{
		"auth_url": oidcSvc.AuthCodeURL(pkce, state, nonce),
		"state":    state,
	})
}

func (h *AdminHandler) OIDCCallback(c *gin.Context) {
	mwSvc := middlewareFromContext(c)
	if mwSvc == nil || mwSvc.OIDC() == nil || !mwSvc.OIDC().Enabled() {
		writeError(c, http.StatusNotImplemented, CodeServiceUnavailable, "OIDC not configured")
		return
	}

	code := c.Query("code")
	if code == "" {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, "missing code")
		return
	}
	state := c.Query("state")
	expectedState, err := c.Cookie("oidc_state")
	if err != nil || state == "" || state != expectedState {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, "invalid state")
		return
	}
	nonce, err := c.Cookie("oidc_nonce")
	if err != nil {
		nonce = ""
	}
	verifier, err := c.Cookie("oidc_verifier")
	if err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, "missing pkce verifier")
		return
	}

	result, err := mwSvc.OIDC().Exchange(c.Request.Context(), code, "", verifier, nonce)
	if err != nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidToken, err.Error())
		return
	}

	identity, ok := middleware.Identity(c)
	tenantID := ""
	if ok && identity != nil {
		tenantID = identity.TenantID
	}

	user, err := mwSvc.OIDC().ProvisionUser(c.Request.Context(), result.Claims, tenantID)
	if err != nil {
		writeInternalError(c, err)
		return
	}

	accessToken, err := mwSvc.JWT().IssueAccessToken(user.ID, user.TenantID, user.Role, 0)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	refreshToken, err := mwSvc.JWT().IssueRefreshToken(user.ID, 0)
	if err != nil {
		writeInternalError(c, err)
		return
	}

	// Clear cookies.
	c.SetCookie("oidc_state", "", -1, "/", "", false, true)
	c.SetCookie("oidc_nonce", "", -1, "/", "", false, true)
	c.SetCookie("oidc_verifier", "", -1, "/", "", false, true)

	h.recordAudit(c, "oidc.callback", "user", user.ID, gin.H{"email": result.Claims.Email})

	if postLoginURL := mwSvc.OIDC().PostLoginURL(); postLoginURL != "" {
		location := fmt.Sprintf("%s#access_token=%s&refresh_token=%s&expires_in=%d",
			postLoginURL,
			url.QueryEscape(accessToken),
			url.QueryEscape(refreshToken),
			900,
		)
		c.Redirect(http.StatusFound, location)
		return
	}

	writeOK(c, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    900,
		"user":          userToResponse(*user),
	})
}

func (h *AdminHandler) OIDCRefresh(c *gin.Context) {
	mwSvc := middlewareFromContext(c)
	if mwSvc == nil || mwSvc.JWT() == nil {
		writeError(c, http.StatusNotImplemented, CodeServiceUnavailable, "JWT not configured")
		return
	}

	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	claims, err := mwSvc.JWT().VerifyRefreshToken(req.RefreshToken)
	if err != nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidToken, err.Error())
		return
	}

	user, err := h.store.GetUser(c.Request.Context(), "", claims.UserID)
	if err != nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidToken, "user not found")
		return
	}

	accessToken, err := mwSvc.JWT().IssueAccessToken(user.ID, user.TenantID, user.Role, 0)
	if err != nil {
		writeInternalError(c, err)
		return
	}

	writeOK(c, gin.H{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   900,
	})
}

func (h *AdminHandler) OIDCLogout(c *gin.Context) {
	// JWT tokens cannot be centrally revoked without a blocklist; this endpoint
	// is a placeholder that clients can call to drop local tokens.
	h.recordAudit(c, "oidc.logout", "session", "", nil)
	writeOKMsg(c, "logged out", nil)
}

// middlewareFromContext extracts the middleware service from gin context if available.
// It is set by server.go during route registration.
func middlewareFromContext(c *gin.Context) *middleware.Middleware {
	v, exists := c.Get("middleware")
	if !exists {
		return nil
	}
	mw, ok := v.(*middleware.Middleware)
	if !ok {
		return nil
	}
	return mw
}
