package responses

import (
	"encoding/json"
	"time"

	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
)

type streamTranscriptCollector struct {
	events []cache.StreamEvent
}

func emitStreamEvent(out chan<- provider.ResponseEvent, collector *streamTranscriptCollector, event provider.ResponseEvent) {
	if collector != nil {
		collector.Add(event)
	}
	out <- event
}

func (c *streamTranscriptCollector) Add(event provider.ResponseEvent) {
	if c == nil {
		return
	}
	cached, ok := streamEventToCacheEvent(event)
	if !ok {
		return
	}
	c.events = append(c.events, cached)
}

func (c *streamTranscriptCollector) Events() []cache.StreamEvent {
	if c == nil || len(c.events) == 0 {
		return nil
	}
	out := make([]cache.StreamEvent, len(c.events))
	copy(out, c.events)
	return out
}

func streamEventToCacheEvent(event provider.ResponseEvent) (cache.StreamEvent, bool) {
	cached := cache.StreamEvent{
		Type:          event.Type,
		Delta:         event.Delta,
		TextDelta:     event.TextDelta,
		ThinkingDelta: event.ThinkingDelta,
		FinishReason:  event.FinishReason,
		OutputIndex:   cloneIntPtr(event.OutputIndex),
		ContentIndex:  cloneIntPtr(event.ContentIndex),
	}
	if event.Response != nil {
		if body, err := json.Marshal(event.Response); err == nil {
			cached.Response = body
		}
	}
	if event.Output != nil {
		if body, err := json.Marshal(event.Output); err == nil {
			cached.Output = body
		}
	}
	if len(event.ToolCalls) > 0 {
		if body, err := json.Marshal(event.ToolCalls); err == nil {
			cached.ToolCalls = body
		}
	}
	if event.Usage != nil {
		cached.Usage = &cache.Usage{
			PromptTokens:     event.Usage.PromptTokens,
			CompletionTokens: event.Usage.CompletionTokens,
			TotalTokens:      event.Usage.TotalTokens,
			CachedTokens:     event.Usage.CachedTokens,
		}
	}
	return cached, cached.Type != "" || cached.Delta != "" || cached.TextDelta != "" || cached.ThinkingDelta != "" ||
		len(cached.Response) > 0 || len(cached.Output) > 0 || len(cached.ToolCalls) > 0 || cached.Usage != nil
}

func cacheEventToStreamEvent(cached cache.StreamEvent) (provider.ResponseEvent, error) {
	event := provider.ResponseEvent{
		Type:          cached.Type,
		Delta:         cached.Delta,
		TextDelta:     cached.TextDelta,
		ThinkingDelta: cached.ThinkingDelta,
		FinishReason:  cached.FinishReason,
		OutputIndex:   cloneIntPtr(cached.OutputIndex),
		ContentIndex:  cloneIntPtr(cached.ContentIndex),
	}
	if len(cached.Response) > 0 {
		var resp provider.Response
		if err := json.Unmarshal(cached.Response, &resp); err != nil {
			return provider.ResponseEvent{}, err
		}
		event.Response = &resp
	}
	if len(cached.Output) > 0 {
		var output provider.ResponseOutput
		if err := json.Unmarshal(cached.Output, &output); err != nil {
			return provider.ResponseEvent{}, err
		}
		event.Output = &output
	}
	if len(cached.ToolCalls) > 0 {
		var calls []provider.ToolCall
		if err := json.Unmarshal(cached.ToolCalls, &calls); err != nil {
			return provider.ResponseEvent{}, err
		}
		event.ToolCalls = calls
	}
	if cached.Usage != nil {
		event.Usage = &provider.Usage{
			PromptTokens:     cached.Usage.PromptTokens,
			CompletionTokens: cached.Usage.CompletionTokens,
			TotalTokens:      cached.Usage.TotalTokens,
			CachedTokens:     cached.Usage.CachedTokens,
		}
	}
	return event, nil
}

func buildStreamCacheEntry(req *provider.ResponseRequest, providerName string, finalResponse *provider.Response, transcript []cache.StreamEvent) *cache.Entry {
	if finalResponse == nil {
		return nil
	}
	body, err := json.Marshal(finalResponse)
	if err != nil {
		return nil
	}
	model := ""
	if req != nil {
		model = req.Model
	}
	return &cache.Entry{
		Response:         body,
		StreamTranscript: cloneStreamTranscript(transcript),
		Stream:           true,
		Model:            model,
		Provider:         providerName,
		Usage: cache.Usage{
			PromptTokens:     finalResponse.Usage.PromptTokens,
			CompletionTokens: finalResponse.Usage.CompletionTokens,
			TotalTokens:      finalResponse.Usage.TotalTokens,
			CachedTokens:     finalResponse.Usage.CachedTokens,
		},
		CreatedAt: time.Now().Unix(),
	}
}

func cloneStreamTranscript(in []cache.StreamEvent) []cache.StreamEvent {
	if len(in) == 0 {
		return nil
	}
	out := make([]cache.StreamEvent, len(in))
	for i, event := range in {
		out[i] = cloneStreamEvent(event)
	}
	return out
}

func cloneStreamEvent(event cache.StreamEvent) cache.StreamEvent {
	cloned := event
	cloned.Response = cloneBytesLocal(event.Response)
	cloned.Output = cloneBytesLocal(event.Output)
	cloned.ToolCalls = cloneBytesLocal(event.ToolCalls)
	cloned.OutputIndex = cloneIntPtr(event.OutputIndex)
	cloned.ContentIndex = cloneIntPtr(event.ContentIndex)
	if event.Usage != nil {
		usage := *event.Usage
		cloned.Usage = &usage
	}
	return cloned
}

func cloneBytesLocal(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneIntPtr(in *int) *int {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
