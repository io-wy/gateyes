package gateway

func NewStaticProxy(upstream string, stripPrefix string) (*UpstreamProxy, error) {
	return NewUpstreamProxy(upstream, "", nil, "", "", "", stripPrefix)
}
