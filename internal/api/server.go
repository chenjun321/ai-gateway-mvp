package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ai-gateway-mvp/internal/auth"
	"ai-gateway-mvp/internal/proxy"
	"ai-gateway-mvp/internal/store"
)

type Server struct {
	store        *store.Store
	provider     proxy.Provider
	relayTimeout time.Duration
}

func NewServer(st *store.Store, provider proxy.Provider, relayTimeout time.Duration) *Server {
	return &Server{store: st, provider: provider, relayTimeout: relayTimeout}
}

func (s *Server) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/", s.dashboard)
	r.GET("/dashboard", s.dashboard)
	r.GET("/openapi.yaml", s.openapi)
	r.GET("/docs", s.docs)

	r.POST("/tenants", s.createTenant)
	r.GET("/tenants", s.listTenants)
	r.POST("/tenants/:tenant_id/keys", s.createAPIKey)
	r.GET("/tenants/:tenant_id/keys", s.listAPIKeys)
	r.PATCH("/tenants/:tenant_id/keys/:key_id", s.updateAPIKey)
	r.GET("/usage", s.queryUsage)
	r.GET("/usage/summary", s.usageSummary)

	r.POST("/v1/chat/completions", s.chatCompletions)
	return r
}

type createTenantRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

func (s *Server) createTenant(c *gin.Context) {
	var req createTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(c, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Scopes) == 0 {
		req.Scopes = []string{"chat:invoke", "model:mock-gpt"}
	}
	id, err := auth.NewID("tenant")
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	tenant, err := s.store.CreateTenant(c.Request.Context(), store.Tenant{
		ID:        id,
		Name:      req.Name,
		Scopes:    uniqueStrings(req.Scopes),
		CreatedAt: time.Now(),
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, tenant)
}

func (s *Server) listTenants(c *gin.Context) {
	tenants, err := s.store.ListTenants(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tenants})
}

type createAPIKeyRequest struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	Enabled   *bool      `json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type createAPIKeyResponse struct {
	store.APIKey
	Key string `json:"key"`
}

func (s *Server) createAPIKey(c *gin.Context) {
	tenantID := c.Param("tenant_id")
	tenant, err := s.store.GetTenant(c.Request.Context(), tenantID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(c, http.StatusNotFound, "tenant not found")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	var req createAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(c, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Scopes) == 0 {
		req.Scopes = tenant.Scopes
	}
	req.Scopes = uniqueStrings(req.Scopes)
	if !auth.IsSubset(req.Scopes, tenant.Scopes) {
		writeError(c, http.StatusBadRequest, "key scopes must be a subset of tenant scopes")
		return
	}

	rawKey, err := auth.GenerateKey()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := auth.NewID("key")
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	key, err := s.store.CreateAPIKey(c.Request.Context(), store.APIKey{
		ID:        id,
		TenantID:  tenantID,
		Name:      req.Name,
		KeyHash:   auth.HashKey(rawKey),
		KeyPrefix: auth.PublicPrefix(rawKey),
		Scopes:    req.Scopes,
		Enabled:   enabled,
		ExpiresAt: req.ExpiresAt,
		CreatedAt: time.Now(),
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, createAPIKeyResponse{APIKey: *key, Key: rawKey})
}

func (s *Server) listAPIKeys(c *gin.Context) {
	tenantID := c.Param("tenant_id")
	if _, err := s.store.GetTenant(c.Request.Context(), tenantID); errors.Is(err, store.ErrNotFound) {
		writeError(c, http.StatusNotFound, "tenant not found")
		return
	} else if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	keys, err := s.store.ListAPIKeys(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": keys})
}

type updateAPIKeyRequest struct {
	Name      *string    `json:"name"`
	Scopes    []string   `json:"scopes"`
	Enabled   *bool      `json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (s *Server) updateAPIKey(c *gin.Context) {
	tenantID := c.Param("tenant_id")
	keyID := c.Param("key_id")
	tenant, err := s.store.GetTenant(c.Request.Context(), tenantID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(c, http.StatusNotFound, "tenant not found")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	key, err := s.store.GetAPIKey(c.Request.Context(), tenantID, keyID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(c, http.StatusNotFound, "key not found")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	var req updateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(c, http.StatusBadRequest, "name cannot be empty")
			return
		}
		key.Name = name
	}
	if req.Scopes != nil {
		req.Scopes = uniqueStrings(req.Scopes)
		if !auth.IsSubset(req.Scopes, tenant.Scopes) {
			writeError(c, http.StatusBadRequest, "key scopes must be a subset of tenant scopes")
			return
		}
		key.Scopes = req.Scopes
	}
	if req.Enabled != nil {
		key.Enabled = *req.Enabled
	}
	if req.ExpiresAt != nil {
		key.ExpiresAt = req.ExpiresAt
	}
	updated, err := s.store.UpdateAPIKey(c.Request.Context(), *key)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (s *Server) chatCompletions(c *gin.Context) {
	var req proxy.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAIError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		writeOpenAIError(c, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeOpenAIError(c, http.StatusBadRequest, "invalid_request", "messages is required")
		return
	}
	if req.Stream {
		writeOpenAIError(c, http.StatusBadRequest, "unsupported_feature", "streaming is not implemented in this MVP")
		return
	}

	apiKey, tenant, ok := s.authenticateGatewayKey(c)
	if !ok {
		return
	}
	if !auth.Allows(tenant.Scopes, "chat:invoke") || !auth.Allows(apiKey.Scopes, "chat:invoke") {
		writeOpenAIError(c, http.StatusForbidden, "forbidden", "key is not allowed to invoke chat completions")
		return
	}
	modelScope := "model:" + req.Model
	if !auth.Allows(tenant.Scopes, modelScope) || !auth.Allows(apiKey.Scopes, modelScope) {
		writeOpenAIError(c, http.StatusForbidden, "forbidden", "key is not allowed to use model "+req.Model)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), s.relayTimeout)
	defer cancel()
	resp, err := s.provider.Chat(ctx, req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			writeOpenAIError(c, http.StatusGatewayTimeout, "upstream_timeout", "downstream provider timed out")
			return
		}
		writeOpenAIError(c, http.StatusBadGateway, "upstream_error", "downstream provider failed")
		return
	}

	usage := store.UsageRecord{
		ID:               mustID("usage"),
		TenantID:         tenant.ID,
		APIKeyID:         apiKey.ID,
		Model:            req.Model,
		ThreadID:         req.ThreadID,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
		CreatedAt:        time.Now(),
	}
	if _, err := s.store.InsertUsage(c.Request.Context(), usage); err != nil {
		writeOpenAIError(c, http.StatusInternalServerError, "usage_record_failed", err.Error())
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Server) authenticateGatewayKey(c *gin.Context) (*store.APIKey, *store.Tenant, bool) {
	raw := auth.BearerToken(c.GetHeader("Authorization"))
	if raw == "" {
		writeOpenAIError(c, http.StatusUnauthorized, "unauthorized", "missing bearer token")
		return nil, nil, false
	}
	key, err := s.store.GetAPIKeyByHash(c.Request.Context(), auth.HashKey(raw))
	if errors.Is(err, store.ErrNotFound) {
		writeOpenAIError(c, http.StatusUnauthorized, "unauthorized", "invalid bearer token")
		return nil, nil, false
	}
	if err != nil {
		writeOpenAIError(c, http.StatusInternalServerError, "auth_failed", err.Error())
		return nil, nil, false
	}
	if !key.Enabled {
		writeOpenAIError(c, http.StatusUnauthorized, "unauthorized", "key is disabled")
		return nil, nil, false
	}
	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		writeOpenAIError(c, http.StatusUnauthorized, "unauthorized", "key is expired")
		return nil, nil, false
	}
	tenant, err := s.store.GetTenant(c.Request.Context(), key.TenantID)
	if err != nil {
		writeOpenAIError(c, http.StatusUnauthorized, "unauthorized", "tenant not found")
		return nil, nil, false
	}
	return key, tenant, true
}

func (s *Server) queryUsage(c *gin.Context) {
	filter, ok := usageFilterFromQuery(c)
	if !ok {
		return
	}
	records, err := s.store.QueryUsage(c.Request.Context(), filter)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": records})
}

func (s *Server) usageSummary(c *gin.Context) {
	filter, ok := usageFilterFromQuery(c)
	if !ok {
		return
	}
	summary, err := s.store.SummarizeUsage(c.Request.Context(), filter)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": summary})
}

func usageFilterFromQuery(c *gin.Context) (store.UsageFilter, bool) {
	filter := store.UsageFilter{
		TenantID: c.Query("tenant_id"),
		APIKeyID: c.Query("api_key_id"),
		Model:    c.Query("model"),
		ThreadID: c.Query("thread_id"),
	}
	if limit := c.Query("limit"); limit != "" {
		parsed, err := strconv.Atoi(limit)
		if err != nil {
			writeError(c, http.StatusBadRequest, "limit must be an integer")
			return filter, false
		}
		filter.Limit = parsed
	}
	if from := c.Query("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			writeError(c, http.StatusBadRequest, "from must be RFC3339")
			return filter, false
		}
		filter.From = &t
	}
	if to := c.Query("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			writeError(c, http.StatusBadRequest, "to must be RFC3339")
			return filter, false
		}
		filter.To = &t
	}
	return filter, true
}

func (s *Server) openapi(c *gin.Context) {
	c.File("openapi.yaml")
}

func (s *Server) docs(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, `<!doctype html>
<html>
<head><title>AI Gateway API Docs</title><link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist/swagger-ui.css"></head>
<body><div id="swagger-ui"></div><script src="https://unpkg.com/swagger-ui-dist/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({url:"/openapi.yaml",dom_id:"#swagger-ui"});</script></body>
</html>`)
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

func writeOpenAIError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    code,
			"code":    code,
		},
	})
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func mustID(prefix string) string {
	id, err := auth.NewID(prefix)
	if err != nil {
		panic(err)
	}
	return id
}

func EnvDurationSeconds(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
