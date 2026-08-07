package responses

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func (s *Service) normalizeResponse(exec *execution, resp *provider.Response) *provider.Response {
	if resp == nil {
		resp = provider.NewTextResponse(exec.responseID, exec.requestedModel, "", provider.Usage{})
	}
	if resp.ID == "" {
		resp.ID = exec.responseID
	} else {
		resp.ID = exec.responseID
	}
	resp.Object = "response"
	if resp.Created == 0 {
		resp.Created = time.Now().Unix()
	}
	resp.Model = exec.requestedModel
	if resp.Status == "" {
		resp.Status = "completed"
	}
	if resp.Usage.PromptTokens == 0 {
		resp.Usage.PromptTokens = exec.estimatedPromptTokens
	}
	if resp.Usage.CompletionTokens == 0 {
		resp.Usage.CompletionTokens = provider.RoughTokenCount(resp.Signature())
	}
	if resp.Usage.TotalTokens == 0 {
		resp.Usage.TotalTokens = resp.Usage.PromptTokens + resp.Usage.CompletionTokens
	}
	return resp
}

func validateVisibleOutputBudget(exec *execution, resp *provider.Response) error {
	if exec == nil || exec.upstreamRequest == nil || resp == nil {
		return nil
	}
	requested := exec.upstreamRequest.RequestedMaxTokens()
	if requested <= 0 {
		return nil
	}
	if hasVisibleOutput(resp) {
		return nil
	}
	if !thinkingOnlyResponse(resp) {
		if requested > 128 {
			return nil
		}
		return fmt.Errorf(
			"%w: upstream produced no visible output; requested_tokens=%d completion_tokens=%d; increase max_tokens/max_output_tokens",
			ErrOutputBudgetTooLow,
			requested,
			resp.Usage.CompletionTokens,
		)
	}
	if !nearOutputBudgetLimit(resp.Usage.CompletionTokens, requested) {
		return nil
	}
	return fmt.Errorf(
		"%w: upstream produced only thinking blocks and no visible output; requested_tokens=%d completion_tokens=%d; increase max_tokens/max_output_tokens",
		ErrOutputBudgetTooLow,
		requested,
		resp.Usage.CompletionTokens,
	)
}

func hasVisibleOutput(resp *provider.Response) bool {
	if resp == nil {
		return false
	}
	return resp.OutputText() != "" || len(resp.OutputToolCalls()) > 0
}

func thinkingOnlyResponse(resp *provider.Response) bool {
	if resp == nil {
		return false
	}
	hasThinking := false
	for _, output := range resp.Output {
		if output.Type == "function_call" {
			return false
		}
		for _, content := range output.Content {
			if content.Text != "" || content.Refusal != "" {
				return false
			}
			if content.Thinking != "" {
				hasThinking = true
			}
		}
	}
	return hasThinking
}

func nearOutputBudgetLimit(actual, requested int) bool {
	if actual <= 0 || requested <= 0 {
		return false
	}
	threshold := int(math.Ceil(float64(requested) * 0.9))
	if threshold < 1 {
		threshold = 1
	}
	return actual >= threshold
}

func (s *Service) recoverStreamResponse(ctx context.Context, identity *repository.AuthIdentity, exec *execution, assistantText string, streamedOutputs []provider.ResponseOutput, finalResponse *provider.Response, hasSentPayload bool) *provider.Response {
	if !hasSentPayload && !hasRenderableStreamPayload(finalResponse) {
		recovered, _, err := s.callWithRetry(context.WithoutCancel(ctx), identity, exec)
		if err == nil && recovered != nil {
			finalResponse = recovered
		}
	}
	if !hasRenderableStreamPayload(finalResponse) && (assistantText != "" || len(streamedOutputs) > 0) {
		finalResponse = buildAccumulatedStreamResponse(exec.responseID, exec.requestedModel, assistantText, streamedOutputs, exec.estimatedPromptTokens)
		return finalResponse
	}
	if finalResponse == nil {
		finalResponse = buildAccumulatedStreamResponse(exec.responseID, exec.requestedModel, assistantText, streamedOutputs, exec.estimatedPromptTokens)
	}
	return finalResponse
}

func applyRecoveredStreamUsage(resp *provider.Response, usage *provider.Usage) {
	if resp == nil || usage == nil {
		return
	}
	if usage.PromptTokens > 0 {
		resp.Usage.PromptTokens = usage.PromptTokens
	}
	if usage.CompletionTokens > 0 {
		resp.Usage.CompletionTokens = usage.CompletionTokens
	}
	if usage.TotalTokens > 0 {
		resp.Usage.TotalTokens = usage.TotalTokens
	} else if resp.Usage.PromptTokens > 0 && resp.Usage.CompletionTokens > 0 {
		resp.Usage.TotalTokens = resp.Usage.PromptTokens + resp.Usage.CompletionTokens
	}
}

func hasRenderableStreamPayload(resp *provider.Response) bool {
	if resp == nil {
		return false
	}
	if resp.OutputText() != "" || len(resp.OutputToolCalls()) > 0 {
		return true
	}
	for _, output := range resp.Output {
		if output.Type == "message" {
			for _, content := range output.Content {
				if content.Text != "" {
					return true
				}
			}
		}
	}
	return false
}

func buildAccumulatedStreamResponse(responseID, model, assistantText string, streamedOutputs []provider.ResponseOutput, estimatedPromptTokens int) *provider.Response {
	outputs := make([]provider.ResponseOutput, 0, len(streamedOutputs)+1)
	if assistantText != "" {
		outputs = append(outputs, provider.ResponseOutput{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []provider.ResponseContent{{
				Type: "output_text",
				Text: assistantText,
			}},
		})
	}
	outputs = append(outputs, streamedOutputs...)
	if len(outputs) == 0 {
		return provider.NewTextResponse(responseID, model, "", provider.Usage{
			PromptTokens:     estimatedPromptTokens,
			CompletionTokens: 0,
			TotalTokens:      estimatedPromptTokens,
		})
	}
	return &provider.Response{
		ID:      responseID,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  outputs,
		Usage: provider.Usage{
			PromptTokens:     estimatedPromptTokens,
			CompletionTokens: provider.RoughTokenCount(assistantText),
			TotalTokens:      estimatedPromptTokens + provider.RoughTokenCount(assistantText),
		},
	}
}

func (s *Service) emitStreamPayloadFromResponse(out chan<- provider.ResponseEvent, resp *provider.Response, collectors ...*streamTranscriptCollector) {
	if resp == nil {
		return
	}
	var collector *streamTranscriptCollector
	if len(collectors) > 0 {
		collector = collectors[0]
	}
	for _, output := range resp.Output {
		switch output.Type {
		case "message":
			for _, content := range output.Content {
				if content.Text == "" {
					continue
				}
				emitStreamEvent(out, collector, provider.ResponseEvent{
					Type:  provider.EventContentDelta,
					Delta: content.Text,
				})
			}
		case "function_call":
			item := output
			emitStreamEvent(out, collector, provider.ResponseEvent{
				Type:   provider.EventToolCallDone,
				Output: &item,
			})
		}
	}
}

func appendStreamedToolCalls(outputs []provider.ResponseOutput, calls []provider.ToolCall) []provider.ResponseOutput {
	for _, call := range calls {
		outputs = appendStreamOutput(outputs, &provider.ResponseOutput{
			ID:     call.ID,
			Type:   "function_call",
			Status: "completed",
			CallID: call.ID,
			Name:   call.Function.Name,
			Args:   call.Function.Arguments,
		})
	}
	return outputs
}

func appendStreamOutput(outputs []provider.ResponseOutput, output *provider.ResponseOutput) []provider.ResponseOutput {
	if output == nil {
		return outputs
	}
	key := firstNonEmptyLocal(output.ID, output.CallID)
	if output.Type == "function_call" && key != "" {
		for _, existing := range outputs {
			existingKey := firstNonEmptyLocal(existing.ID, existing.CallID)
			if existing.Type == output.Type && existingKey == key {
				return outputs
			}
		}
	}
	cloned := *output
	return append(outputs, cloned)
}

func firstNonEmptyLocal(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func isRenderableStreamEvent(event provider.ResponseEvent) bool {
	if event.Text() != "" {
		return true
	}
	if len(event.ToolCalls) > 0 {
		return true
	}
	if event.Output != nil && event.Output.Type == "function_call" {
		return true
	}
	return false
}
