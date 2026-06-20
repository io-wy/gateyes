package gateerror

import (
	stderrors "errors"
	"testing"
)

func TestWrapAndCodeHelpers(t *testing.T) {
	root := stderrors.New("boom")
	err := Wrap("provider.openai.create_response", CodeTransport, root, "request failed")

	if err == nil {
		t.Fatal("Wrap() returned nil")
	}
	if got := CodeOf(err); got != CodeTransport {
		t.Fatalf("CodeOf() = %q, want %q", got, CodeTransport)
	}
	if !IsCode(err, CodeTransport) {
		t.Fatal("IsCode() = false, want true")
	}
	if err.Unwrap() != root {
		t.Fatalf("Unwrap() = %v, want %v", err.Unwrap(), root)
	}
}

func TestNewDefaultsUnknownCode(t *testing.T) {
	err := New("", "plain message")
	if got := CodeOf(err); got != CodeUnknown {
		t.Fatalf("CodeOf(New) = %q, want %q", got, CodeUnknown)
	}
	if err.Error() == "" {
		t.Fatal("Error() returned empty string")
	}
}
