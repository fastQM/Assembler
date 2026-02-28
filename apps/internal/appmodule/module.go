package appmodule

import "net/http"

// Module is a same-process app plugin mounted by apps-web.
type Module interface {
	ID() string
	Mount(mux *http.ServeMux)
	Health() error
}
