package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

const maxPreviousResponseDepth = 32

type previousResponseHydratedKey struct{}

func withPreviousResponseHydrated(ctx context.Context) context.Context {
	return context.WithValue(ctx, previousResponseHydratedKey{}, true)
}

func previousResponseHydrated(ctx context.Context) bool {
	hydrated, _ := ctx.Value(previousResponseHydratedKey{}).(bool)
	return hydrated
}

func requestBodyBeforeHydration(ctx context.Context, req *provider.ResponseRequest) []byte {
	if req == nil || req.PreviousResponseID == "" {
		return nil
	}
	if raw := rawBodyFromContext(ctx); len(raw) > 0 {
		return append([]byte(nil), raw...)
	}
	body, _ := json.Marshal(req)
	return body
}

func (s *Service) hydratePreviousResponse(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest) error {
	if req == nil || req.PreviousResponseID == "" {
		return nil
	}
	if s.store == nil || identity == nil {
		return fmt.Errorf("%w: response store unavailable", ErrInvalidPreviousResponse)
	}

	seen := make(map[string]struct{}, maxPreviousResponseDepth)
	currentID := req.PreviousResponseID
	turns := make([][]provider.Message, 0, 4)
	for depth := 0; currentID != ""; depth++ {
		if depth >= maxPreviousResponseDepth {
			return fmt.Errorf("%w: chain exceeds %d responses", ErrInvalidPreviousResponse, maxPreviousResponseDepth)
		}
		if _, exists := seen[currentID]; exists {
			return fmt.Errorf("%w: response chain contains a cycle", ErrInvalidPreviousResponse)
		}
		seen[currentID] = struct{}{}

		record, err := s.store.GetResponse(ctx, identity.TenantID, currentID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return fmt.Errorf("%w: response %q not found", ErrInvalidPreviousResponse, currentID)
			}
			return fmt.Errorf("hydrate previous response %q: %w", currentID, err)
		}
		if record.ProjectID != identity.ProjectID {
			return fmt.Errorf("%w: response %q not found in current scope", ErrInvalidPreviousResponse, currentID)
		}
		if record.Status != "completed" {
			return fmt.Errorf("%w: response %q is not completed", ErrInvalidPreviousResponse, currentID)
		}

		var previousRequest provider.ResponseRequest
		if err := json.Unmarshal(record.RequestBody, &previousRequest); err != nil {
			return fmt.Errorf("%w: response %q has an invalid request body", ErrInvalidPreviousResponse, currentID)
		}
		var previousResponse provider.Response
		if err := json.Unmarshal(record.ResponseBody, &previousResponse); err != nil {
			return fmt.Errorf("%w: response %q has an invalid response body", ErrInvalidPreviousResponse, currentID)
		}
		turn := append(previousRequest.InputMessages(), responseMessages(&previousResponse)...)
		turns = append(turns, turn)
		currentID = previousRequest.PreviousResponseID
	}

	messages := make([]provider.Message, 0)
	for i := len(turns) - 1; i >= 0; i-- {
		messages = append(messages, turns[i]...)
	}
	messages = append(messages, req.InputMessages()...)
	req.Messages = messages
	req.Input = messages
	return nil
}

func responseMessages(resp *provider.Response) []provider.Message {
	if resp == nil {
		return nil
	}
	result := make([]provider.Message, 0, len(resp.Output))
	for _, output := range resp.Output {
		message := provider.Message{Role: output.Role, Type: output.Type, Name: output.Name}
		if message.Role == "" {
			message.Role = "assistant"
		}
		for _, content := range output.Content {
			message.Content = append(message.Content, provider.ContentBlock{
				Type:       content.Type,
				Text:       content.Text,
				Thinking:   content.Thinking,
				Signature:  content.Signature,
				Refusal:    content.Refusal,
				Image:      content.Image,
				Structured: content.Structured,
			})
		}
		if output.Type == "function_call" {
			message.ToolCalls = []provider.ToolCall{{
				ID:       output.CallID,
				Type:     "function",
				Function: provider.FunctionCall{Name: output.Name, Arguments: output.Args},
			}}
		}
		if len(message.Content) > 0 || len(message.ToolCalls) > 0 {
			result = append(result, message)
		}
	}
	return result
}
