package provider

import "testing"

func TestChatStreamEncoderAdditionalBranches(t *testing.T) {
	encoder := NewChatStreamEncoder("resp-1", "gpt-test")
	if got := encoder.Encode(ResponseEvent{Type: EventResponseStarted}); got != nil {
		t.Fatalf("Encode(response_started) = %+v, want nil", got)
	}
	if got := encoder.Encode(ResponseEvent{Type: EventContentDelta}); got != nil {
		t.Fatalf("Encode(empty content delta) = %+v, want nil", got)
	}
	if got := encoder.Encode(ResponseEvent{Type: EventToolCallDone, Output: nil}); got != nil {
		t.Fatalf("Encode(tool_call_done nil output) = %+v, want nil", got)
	}
	if got := encoder.Encode(ResponseEvent{Type: EventToolCallDone, Output: &ResponseOutput{Type: "message"}}); got != nil {
		t.Fatalf("Encode(tool_call_done non-function) = %+v, want nil", got)
	}

	completed := encoder.Encode(ResponseEvent{Type: EventResponseCompleted, Response: &Response{}})
	if len(completed) != 1 {
		t.Fatalf("Encode(response_completed first) = %+v, want one completion chunk", completed)
	}
	encoder.finished = true
	if got := encoder.Encode(ResponseEvent{Type: EventContentDelta, TextDelta: "later"}); got != nil {
		t.Fatalf("Encode(content_delta when finished) = %+v, want nil", got)
	}
	if got := encoder.Encode(ResponseEvent{Type: EventToolCallDone, Output: &ResponseOutput{Type: "function_call"}}); got != nil {
		t.Fatalf("Encode(tool_call_done when finished) = %+v, want nil", got)
	}
	if got := encoder.Encode(ResponseEvent{Type: EventResponseCompleted, Response: &Response{}}); got != nil {
		t.Fatalf("Encode(response_completed after finished) = %+v, want nil", got)
	}
	if got := encoder.Encode(ResponseEvent{Type: "unknown"}); got != nil {
		t.Fatalf("Encode(unknown) = %+v, want nil", got)
	}
}
