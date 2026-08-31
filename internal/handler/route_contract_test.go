package handler

import (
	"net/http"
	"strings"
	"testing"
)

type contractRoute struct {
	method string
	path   string
}

func TestContractRouteAdminV1Manifest(t *testing.T) {
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	routes := contractRouteMap(env.server)

	for _, route := range adminContractRoutes() {
		key := route.method + " /admin/v1" + route.path
		if _, ok := routes[key]; !ok {
			t.Errorf("missing Admin v1 route %s", key)
		}
	}

	if got, want := countContractRoutes(routes, "/admin/v1"), len(adminContractRoutes()); got != want {
		t.Fatalf("Admin v1 route count = %d, want %d", got, want)
	}
}

func TestContractLegacyAliasesMatchAdminV1(t *testing.T) {
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	routes := contractRouteMap(env.server)

	for _, route := range adminContractRoutes() {
		v1Key := route.method + " /admin/v1" + route.path
		legacyKey := route.method + " /admin" + route.path
		v1Handler, v1OK := routes[v1Key]
		legacyHandler, legacyOK := routes[legacyKey]
		if !v1OK {
			t.Errorf("missing Admin v1 route %s", v1Key)
			continue
		}
		if !legacyOK {
			t.Errorf("missing legacy alias %s", legacyKey)
			continue
		}
		if legacyHandler != v1Handler {
			t.Errorf("legacy alias %s handler = %q, want %q", legacyKey, legacyHandler, v1Handler)
		}
	}

	if got, want := countContractRoutes(routes, "/admin"), len(adminContractRoutes()); got != want {
		t.Fatalf("legacy Admin route count = %d, want %d", got, want)
	}
}

func contractRouteMap(server *Server) map[string]string {
	routes := make(map[string]string)
	for _, route := range server.engine.Routes() {
		routes[route.Method+" "+route.Path] = route.Handler
	}
	return routes
}

func countContractRoutes(routes map[string]string, prefix string) int {
	count := 0
	for key := range routes {
		path := strings.SplitN(key, " ", 2)[1]
		switch prefix {
		case "/admin/v1":
			if strings.HasPrefix(path, "/admin/v1/") {
				count++
			}
		case "/admin":
			if strings.HasPrefix(path, "/admin/") &&
				!strings.HasPrefix(path, "/admin/v1/") &&
				!strings.HasPrefix(path, "/admin/auth/") {
				count++
			}
		}
	}
	return count
}

func adminContractRoutes() []contractRoute {
	return []contractRoute{
		{http.MethodGet, "/me"},
		{http.MethodGet, "/dashboard"},
		{http.MethodGet, "/catalog"},
		{http.MethodGet, "/cache/summary"},
		{http.MethodGet, "/providers"},
		{http.MethodPost, "/providers/check"},
		{http.MethodPost, "/providers"},
		{http.MethodGet, "/providers/:name"},
		{http.MethodGet, "/providers/:name/stats"},
		{http.MethodPut, "/providers/:name"},
		{http.MethodDelete, "/providers/:name"},
		{http.MethodGet, "/audit"},
		{http.MethodGet, "/services"},
		{http.MethodPost, "/services"},
		{http.MethodGet, "/services/:id"},
		{http.MethodPut, "/services/:id"},
		{http.MethodDelete, "/services/:id"},
		{http.MethodGet, "/services/:id/versions"},
		{http.MethodPost, "/services/:id/versions"},
		{http.MethodPost, "/services/:id/publish"},
		{http.MethodPost, "/services/:id/promote"},
		{http.MethodPost, "/services/:id/rollback"},
		{http.MethodGet, "/services/:id/subscriptions"},
		{http.MethodPost, "/services/:id/subscriptions"},
		{http.MethodGet, "/subscriptions/:id"},
		{http.MethodPost, "/subscriptions/:id/review"},
		{http.MethodGet, "/plugins"},
		{http.MethodPost, "/plugins"},
		{http.MethodPost, "/plugins/upload"},
		{http.MethodGet, "/plugins/:id"},
		{http.MethodPut, "/plugins/:id"},
		{http.MethodDelete, "/plugins/:id"},
		{http.MethodGet, "/keys"},
		{http.MethodPost, "/keys"},
		{http.MethodGet, "/keys/:id"},
		{http.MethodPut, "/keys/:id"},
		{http.MethodPost, "/keys/:id/rotate"},
		{http.MethodPost, "/keys/:id/revoke"},
		{http.MethodGet, "/virtual-keys"},
		{http.MethodPost, "/virtual-keys"},
		{http.MethodGet, "/virtual-keys/:id"},
		{http.MethodPut, "/virtual-keys/:id"},
		{http.MethodDelete, "/virtual-keys/:id"},
		{http.MethodGet, "/users"},
		{http.MethodPost, "/users"},
		{http.MethodGet, "/users/:id"},
		{http.MethodPut, "/users/:id"},
		{http.MethodDelete, "/users/:id"},
		{http.MethodPost, "/users/:id/reset"},
		{http.MethodGet, "/users/:id/usage"},
		{http.MethodGet, "/projects"},
		{http.MethodPost, "/projects"},
		{http.MethodGet, "/projects/:id"},
		{http.MethodGet, "/projects/:id/usage"},
		{http.MethodPut, "/projects/:id"},
		{http.MethodDelete, "/projects/:id"},
		{http.MethodGet, "/responses"},
		{http.MethodGet, "/responses/:id"},
		{http.MethodGet, "/responses/:id/trace"},
		{http.MethodGet, "/budgets"},
		{http.MethodGet, "/usage/summary"},
		{http.MethodGet, "/usage/breakdown"},
		{http.MethodGet, "/usage/trend"},
		{http.MethodPost, "/reload"},
		{http.MethodPost, "/sync/router"},
		{http.MethodPost, "/sync/budget"},
		{http.MethodGet, "/roles"},
		{http.MethodPost, "/roles"},
		{http.MethodGet, "/roles/:id"},
		{http.MethodPut, "/roles/:id"},
		{http.MethodDelete, "/roles/:id"},
		{http.MethodGet, "/permissions"},
		{http.MethodGet, "/tenants"},
		{http.MethodPost, "/tenants"},
		{http.MethodGet, "/tenants/:id"},
		{http.MethodPut, "/tenants/:id"},
		{http.MethodDelete, "/tenants/:id"},
		{http.MethodPost, "/tenants/:id/providers"},
	}
}
