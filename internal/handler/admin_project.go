package handler

import (
	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (h *AdminHandler) CreateProject(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	var req CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	tenantID, ok := h.resolveTargetTenant(c, identity, req.TenantID)
	if !ok {
		return
	}
	project, err := h.store.CreateProject(c.Request.Context(), repository.CreateProjectParams{
		TenantID:  tenantID,
		Slug:      req.Slug,
		Name:      req.Name,
		Status:    repository.StatusActive,
		BudgetUSD: req.BudgetUSD,
		Policy:    req.Policy,
	})
	if err != nil {
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "project.create", "project", project.ID, req)
	writeOK(c, projectToResponse(*project))
}

func (h *AdminHandler) ListProjects(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	projects, err := h.store.ListProjects(c.Request.Context(), tenantID)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(projects))
	for _, item := range projects {
		result = append(result, projectToResponse(item))
	}
	writeOK(c, result)
}

func (h *AdminHandler) GetProject(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	project, err := h.store.GetProject(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeProjectNotFound, "project not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, projectToResponse(*project))
}

func (h *AdminHandler) GetProjectUsage(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	project, err := h.store.GetProject(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeProjectNotFound, "project not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	days := 7
	if d := c.Query("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}
	summary, err := h.store.GetProjectUsageSummary(c.Request.Context(), project.TenantID, project.ID)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	trend, err := h.store.GetProjectUsageTrend(c.Request.Context(), project.TenantID, project.ID, days)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	writeOK(c, gin.H{
		"project": projectToResponse(*project),
		"summary": summary,
		"trend":   trend,
	})
}

type UpdateProjectRequest struct {
	Name         *string                         `json:"name"`
	Status       *string                         `json:"status"`
	BudgetUSD    *float64                        `json:"budget_usd"`
	BudgetPolicy *string                         `json:"budget_policy"`
	Policy       *repository.ServicePolicyConfig `json:"policy"`
}

func (h *AdminHandler) UpdateProject(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	var req UpdateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	project, err := h.store.UpdateProject(c.Request.Context(), tenantID, c.Param("id"), repository.UpdateProjectParams{
		Name:         req.Name,
		Status:       req.Status,
		BudgetUSD:    req.BudgetUSD,
		BudgetPolicy: req.BudgetPolicy,
		Policy:       req.Policy,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeProjectNotFound, "project not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.invalidateProjectIdentities(project.ID)
	writeOK(c, projectToResponse(*project))
}

func (h *AdminHandler) DeleteProject(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	project, err := h.store.GetProject(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeProjectNotFound, "project not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	if err := h.store.DeleteProject(c.Request.Context(), tenantID, project.ID); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeProjectNotFound, "project not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.invalidateProjectIdentities(project.ID)
	h.recordAudit(c, "project.delete", "project", project.ID, gin.H{"project_id": project.ID})
	writeOK(c, gin.H{"id": project.ID, "deleted": true})
}

func projectToResponse(project repository.ProjectRecord) gin.H {
	return gin.H{
		"id":            project.ID,
		"tenant_id":     project.TenantID,
		"tenant_slug":   project.TenantSlug,
		"slug":          project.Slug,
		"name":          project.Name,
		"status":        project.Status,
		"budget_usd":    project.BudgetUSD,
		"spent_usd":     project.SpentUSD,
		"budget_policy": project.BudgetPolicy,
		"policy":        project.Policy,
		"created_at":    project.CreatedAt,
		"updated_at":    project.UpdatedAt,
	}
}
