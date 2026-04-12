package router

type RouteContext struct {
	Model               string
	SessionID           string
	InputText           string
	PromptTokens        int
	Stream              bool
	HasTools            bool
	HasImages           bool
	HasStructuredOutput bool
}
