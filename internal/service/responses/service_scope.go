package responses

import "context"

type ServiceCacheScope struct {
	TenantID        string
	ProjectID       string
	ServiceID       string
	RequestPrefix   string
	DefaultProvider string
	DefaultModel    string
	Metadata        map[string]any
}

type serviceCacheScopeKey struct{}

func WithServiceCacheScope(ctx context.Context, scope ServiceCacheScope) context.Context {
	if scope.ServiceID == "" && scope.RequestPrefix == "" && scope.DefaultProvider == "" && scope.DefaultModel == "" && len(scope.Metadata) == 0 {
		return ctx
	}
	return context.WithValue(ctx, serviceCacheScopeKey{}, scope)
}

func ServiceCacheScopeFrom(ctx context.Context) (ServiceCacheScope, bool) {
	if ctx == nil {
		return ServiceCacheScope{}, false
	}
	scope, ok := ctx.Value(serviceCacheScopeKey{}).(ServiceCacheScope)
	return scope, ok
}
