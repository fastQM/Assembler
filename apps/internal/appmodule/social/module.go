package socialmodule

import (
	"net/http"

	"Assembler-Apps/internal/appmodule"
	"Assembler-Apps/internal/socialapi"
)

var _ appmodule.Module = (*Module)(nil)

type Module struct {
	api *socialapi.Server
}

func New(api *socialapi.Server) *Module {
	return &Module{api: api}
}

func (m *Module) ID() string { return "social" }

func (m *Module) Mount(mux *http.ServeMux) {
	// Plugin-style namespaced route for same-process app management.
	m.api.RegisterWithBase(mux, "/api/apps/social/v1")
	// Keep existing route for frontend backward compatibility.
	m.api.RegisterWithBase(mux, "/api/social/v1")
}

func (m *Module) Health() error { return nil }
