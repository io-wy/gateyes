package filter

// Pipeline runs a chain of filters in registration order.
// The first non-Allow result terminates the chain and is returned.
type Pipeline struct {
	chain []Filter
}

// NewPipeline constructs a Pipeline from the given filter chain.
// nil and empty chains are valid (no-op pipeline).
func NewPipeline(chain []Filter) *Pipeline {
	if chain == nil {
		return &Pipeline{}
	}
	return &Pipeline{chain: append([]Filter(nil), chain...)}
}

// Execute runs each filter in order. Returns the first Block result,
// or Allow if all filters pass.
func (p *Pipeline) Execute(req *RequestContext) Result {
	if p == nil || len(p.chain) == 0 {
		return Result{Action: Allow}
	}
	for _, f := range p.chain {
		res := f.Process(req)
		if res.Action != Allow {
			return res
		}
	}
	return Result{Action: Allow}
}
