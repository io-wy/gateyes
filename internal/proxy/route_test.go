package proxy

import (
	"net/http"
	"testing"
)

func TestRouteRule_Match_ExactPath(t *testing.T) {
	r := RouteRule{Host: "api.example.com", Path: "/v1/users", PathType: PathTypeExact}
	req := mustReq("http://api.example.com/v1/users")
	if !r.Match(req) {
		t.Error("expected Match = true for exact path")
	}
	req2 := mustReq("http://api.example.com/v1/users/123")
	if r.Match(req2) {
		t.Error("expected Match = false for subpath")
	}
}

func TestRouteRule_Match_PrefixPath(t *testing.T) {
	r := RouteRule{Host: "", Path: "/api", PathType: PathTypePrefix}
	req := mustReq("http://any.host/api/v1/users")
	if !r.Match(req) {
		t.Error("expected Match = true for prefix path")
	}
	req2 := mustReq("http://any.host/other")
	if r.Match(req2) {
		t.Error("expected Match = false for different path")
	}
}

func TestRouteRule_Match_HostWildcard(t *testing.T) {
	r := RouteRule{Host: "*.example.com", Path: "/", PathType: PathTypePrefix}
	cases := []struct {
		host string
		want bool
	}{
		{"api.example.com", true},
		{"v1.api.example.com", true},
		{"example.com", false},
		{"other.com", false},
	}
	for _, c := range cases {
		req := mustReq("http://" + c.host + "/")
		if got := r.Match(req); got != c.want {
			t.Errorf("Match(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestRouteRule_Match_PortStripped(t *testing.T) {
	r := RouteRule{Host: "api.example.com", Path: "/", PathType: PathTypePrefix}
	req := mustReq("http://api.example.com:8080/health")
	if !r.Match(req) {
		t.Error("expected Match = true with port stripped")
	}
}

func TestRouteRule_RewritePath(t *testing.T) {
	r := RouteRule{
		Path:     "/api/",
		PathType: PathTypePrefix,
		Annotations: &Annotations{RewriteTarget: "/v2/"},
	}
	if got, want := r.RewritePath("/api/users"), "/v2/users"; got != want {
		t.Errorf("RewritePath = %q, want %q", got, want)
	}
}

func TestRouteRule_RewritePath_RootTarget(t *testing.T) {
	r := RouteRule{
		Path:     "/api/",
		PathType: PathTypePrefix,
		Annotations: &Annotations{RewriteTarget: "/"},
	}
	if got, want := r.RewritePath("/api/users"), "users"; got != want {
		t.Errorf("RewritePath = %q, want %q", got, want)
	}
}

func TestRouteRule_RewritePath_NoAnnotation(t *testing.T) {
	r := RouteRule{Path: "/api/", PathType: PathTypePrefix}
	if got, want := r.RewritePath("/api/users"), "/api/users"; got != want {
		t.Errorf("RewritePath = %q, want %q", got, want)
	}
}

func TestRouteRule_UpstreamURL(t *testing.T) {
	b := NewBackend("b1", "10.0.0.1:8080", "http", 1)
	r := RouteRule{
		Path:        "/api/",
		PathType:    PathTypePrefix,
		Annotations: &Annotations{RewriteTarget: "/v2/"},
	}
	got := r.UpstreamURL(b, "/api/users")
	want := "http://10.0.0.1:8080/v2/users"
	if got != want {
		t.Errorf("UpstreamURL = %q, want %q", got, want)
	}
}

func TestRouteRule_UpstreamURL_HTTPS(t *testing.T) {
	b := NewBackend("b1", "10.0.0.1:443", "https", 1)
	r := RouteRule{Path: "/", PathType: PathTypePrefix}
	got := r.UpstreamURL(b, "/health")
	want := "https://10.0.0.1:443/health"
	if got != want {
		t.Errorf("UpstreamURL = %q, want %q", got, want)
	}
}

func mustReq(urlStr string) *http.Request {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		panic(err)
	}
	return req
}
