// Package administration contains adapters for administration application use
// cases. Keeping these wrappers small allows a later transport split without
// changing the existing service behavior.
package administration

import (
	"github.com/gateyes/gateway/internal/ports"
	"github.com/gateyes/gateway/internal/service/adminconsole"
	"github.com/gateyes/gateway/internal/service/catalog"
)

type ConsoleAdapter struct{ *adminconsole.Service }

func NewConsole(service *adminconsole.Service) *ConsoleAdapter {
	return &ConsoleAdapter{Service: service}
}

type CatalogAdapter struct{ *catalog.Service }

func NewCatalog(service *catalog.Service) *CatalogAdapter {
	return &CatalogAdapter{Service: service}
}

var _ ports.AdminConsoleUseCase = (*ConsoleAdapter)(nil)
var _ ports.CatalogUseCase = (*CatalogAdapter)(nil)
