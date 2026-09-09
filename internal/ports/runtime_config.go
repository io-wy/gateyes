package ports

import "context"

// RuntimeConfigPort abstracts live configuration reloads from HTTP handlers.
type RuntimeConfigPort interface {
	Reload(context.Context) error
}

type RuntimeConfigUseCase = RuntimeConfigPort
