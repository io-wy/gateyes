package ingress

import (
	"net/http"
	"testing"

	"github.com/gateyes/gateway/internal/proxy"
)

func TestRouteTable_Lookup_ExactHostAndPath(t *testing.T) {
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{Host: "api.example.com", Path: "/v1/users", PathType: proxy.PathTypeExact, BackendPool: proxy.NewBackendPool(nil)},
	})

	req := mustReq("http://api.example.com/v1/users")
	rule := rt.Lookup(req)
	if rule == nil {
		t.Fatal("expected match")
	}
	if rule.Path != "/v1/users" {
		t.Errorf("Path = %q, want /v1/users", rule.Path)
	}
}

func TestRouteTable_Lookup_PrefixFallback(t *testing.T) {
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{Host: "api.example.com", Path: "/v1", PathType: proxy.PathTypePrefix, BackendPool: proxy.NewBackendPool(nil)},
		{Host: "api.example.com", Path: "/v1/users", PathType: proxy.PathTypeExact, BackendPool: proxy.NewBackendPool(nil)},
	})

	req := mustReq("http://api.example.com/v1/users/123")
	rule := rt.Lookup(req)
	if rule == nil {
		t.Fatal("expected match")
	}
	// Exact rule exists but subpath should still match prefix.
	if rule.PathType != proxy.PathTypePrefix {
		t.Errorf("expected prefix match for subpath, got %v", rule.PathType)
	}
}

func TestRouteTable_Lookup_WildcardHost(t *testing.T) {
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{Host: "*.example.com", Path: "/", PathType: proxy.PathTypePrefix, BackendPool: proxy.NewBackendPool(nil)},
	})

	req := mustReq("http://api.example.com/health")
	rule := rt.Lookup(req)
	if rule == nil {
		t.Fatal("expected wildcard host match")
	}
}

func TestRouteTable_Lookup_NoMatch(t *testing.T) {
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{Host: "api.example.com", Path: "/api", PathType: proxy.PathTypePrefix, BackendPool: proxy.NewBackendPool(nil)},
	})

	req := mustReq("http://other.com/api")
	rule := rt.Lookup(req)
	if rule != nil {
		t.Error("expected no match for different host")
	}
}

func TestRouteTable_Lookup_PriorityExactOverPrefix(t *testing.T) {
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{Host: "api.example.com", Path: "/api", PathType: proxy.PathTypePrefix, BackendPool: proxy.NewBackendPool(nil)},
		{Host: "api.example.com", Path: "/api/v1", PathType: proxy.PathTypeExact, BackendPool: proxy.NewBackendPool(nil)},
	})

	req := mustReq("http://api.example.com/api/v1")
	rule := rt.Lookup(req)
	if rule == nil {
		t.Fatal("expected match")
	}
	if rule.PathType != proxy.PathTypeExact {
		t.Error("expected exact path to win over prefix")
	}
}

func TestRouteTable_Replace_Atomic(t *testing.T) {
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{Host: "a.com", Path: "/", PathType: proxy.PathTypePrefix, BackendPool: proxy.NewBackendPool(nil)},
	})
	rt.Replace([]proxy.RouteRule{
		{Host: "b.com", Path: "/", PathType: proxy.PathTypePrefix, BackendPool: proxy.NewBackendPool(nil)},
	})

	list := rt.List()
	if len(list) != 1 {
		t.Fatalf("List() len = %d, want 1", len(list))
	}
	if list[0].Host != "b.com" {
		t.Errorf("Host = %q, want b.com", list[0].Host)
	}
}

func TestRouteTable_List_Snapshot(t *testing.T) {
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{Host: "a.com", Path: "/", PathType: proxy.PathTypePrefix, BackendPool: proxy.NewBackendPool(nil)},
		{Host: "b.com", Path: "/", PathType: proxy.PathTypePrefix, BackendPool: proxy.NewBackendPool(nil)},
	})

	list := rt.List()
	if len(list) != 2 {
		t.Fatalf("List() len = %d, want 2", len(list))
	}
}

func mustReq(urlStr string) *http.Request {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		panic(err)
	}
	return req
}
