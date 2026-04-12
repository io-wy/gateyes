package provider

import "testing"

func TestParseOpenAIResponseEventHandlesResponsesFailure(t *testing.T) {
	event, err := parseOpenAIResponseEvent(`{"type":"response.failed","response":{"error":{"message":"upstream exploded"}}}`, "public-model")
	if err == nil || err.Error() != "upstream exploded" {
		t.Fatalf("parseOpenAIResponseEvent(response.failed) = (%+v,%v), want upstream exploded error", event, err)
	}
	if event != nil {
		t.Fatalf("parseOpenAIResponseEvent(response.failed) event = %+v, want nil", event)
	}
}

func TestConvertOpenAIResponseAcceptsNullOutput(t *testing.T) {
	resp := convertOpenAIResponse(openAIResponsePayload{
		ID:        "resp-null-output",
		Object:    "response",
		CreatedAt: 123,
		Model:     "provider-model",
		Status:    "completed",
		Output:    nil,
	}, "public-model")

	if resp == nil {
		t.Fatal("convertOpenAIResponse(nil output) = nil, want normalized response")
	}
	if resp.Model != "public-model" || resp.Status != "completed" {
		t.Fatalf("convertOpenAIResponse(nil output) = %+v, want preserved model/status", resp)
	}
	if len(resp.Output) != 0 {
		t.Fatalf("convertOpenAIResponse(nil output) output = %+v, want empty slice", resp.Output)
	}
}
