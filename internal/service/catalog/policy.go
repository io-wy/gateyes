package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

func (s *Service) applyRequestPolicy(policy *repository.ServicePolicyConfig, req *provider.ResponseRequest) error {
	if policy == nil || !policy.Enabled || policy.Request == nil || req == nil {
		return nil
	}
	rules := policy.Request
	if len(rules.AllowModels) > 0 && !containsString(rules.AllowModels, req.Model) {
		return fmt.Errorf("%w: model not in allowlist", ErrPolicyViolation)
	}
	if containsString(rules.BlockModels, req.Model) {
		return fmt.Errorf("%w: model blocked", ErrPolicyViolation)
	}
	text := req.InputText()
	if rules.MaxInputChars > 0 && len(text) > rules.MaxInputChars {
		return fmt.Errorf("%w: input exceeds max_input_chars", ErrPolicyViolation)
	}
	if err := checkBlockedContent(rules, text); err != nil {
		return err
	}
	if len(rules.RedactTerms) > 0 {
		for i := range req.Messages {
			for j := range req.Messages[i].Content {
				if req.Messages[i].Content[j].Text != "" {
					req.Messages[i].Content[j].Text = redactText(req.Messages[i].Content[j].Text, rules.RedactTerms)
				}
			}
		}
		req.Input = req.InputMessages()
	}
	return nil
}

func (s *Service) applyResponsePolicy(ctx context.Context, identity *repository.AuthIdentity, runtime *serviceRuntime, resp *provider.Response) error {
	if runtime == nil || runtime.snapshot.Config.Policy == nil || !runtime.snapshot.Config.Policy.Enabled || runtime.snapshot.Config.Policy.Response == nil || resp == nil {
		return nil
	}
	rules := runtime.snapshot.Config.Policy.Response
	text := resp.OutputText()
	if err := checkBlockedContent(rules, text); err != nil {
		_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
			ID:           resp.ID,
			TenantID:     identity.TenantID,
			ProjectID:    identity.ProjectID,
			ProviderName: runtime.snapshot.DefaultProvider,
			Model:        resp.Model,
			Status:       "error",
		})
		return err
	}
	if rules.MaxOutputChars > 0 && len(text) > rules.MaxOutputChars {
		return fmt.Errorf("%w: output exceeds max_output_chars", ErrPolicyViolation)
	}
	if len(rules.RedactTerms) > 0 {
		for i := range resp.Output {
			for j := range resp.Output[i].Content {
				if resp.Output[i].Content[j].Text != "" {
					resp.Output[i].Content[j].Text = redactText(resp.Output[i].Content[j].Text, rules.RedactTerms)
				}
			}
		}
		body, _ := json.Marshal(resp)
		_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
			ID:           resp.ID,
			TenantID:     identity.TenantID,
			ProjectID:    identity.ProjectID,
			ProviderName: runtime.snapshot.DefaultProvider,
			Model:        resp.Model,
			Status:       "completed",
			ResponseBody: body,
		})
	}
	return nil
}

func (s *Service) filterResponseStream(ctx context.Context, runtime *serviceRuntime, stream *responseSvc.Stream, out chan<- provider.ResponseEvent, errCh chan<- error) {
	defer close(out)
	defer close(errCh)

	var buffer bytes.Buffer
	policy := runtime.snapshot.Config.Policy.Response

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				return
			}
			if event.Type == provider.EventContentDelta && event.Text() != "" {
				text := event.Text()
				if len(policy.RedactTerms) > 0 {
					text = redactText(text, policy.RedactTerms)
				}
				buffer.WriteString(text)
				if err := checkBlockedContent(policy, buffer.String()); err != nil {
					errCh <- err
					return
				}
				if policy.MaxOutputChars > 0 && buffer.Len() > policy.MaxOutputChars {
					errCh <- fmt.Errorf("%w: output exceeds max_output_chars", ErrPolicyViolation)
					return
				}
				event.Delta = text
				event.TextDelta = text
			}
			if event.Type == provider.EventResponseCompleted && event.Response != nil {
				if err := s.applyResponsePolicy(ctx, &repository.AuthIdentity{TenantID: runtime.service.TenantID, ProjectID: runtime.service.ProjectID}, runtime, event.Response); err != nil {
					errCh <- err
					return
				}
			}
			out <- event
		case err, ok := <-stream.Errors:
			if ok && err != nil {
				errCh <- err
			}
			return
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		}
	}
}
