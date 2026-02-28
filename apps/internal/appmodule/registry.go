package appmodule

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu      sync.RWMutex
	modules map[string]Module
}

func NewRegistry() *Registry {
	return &Registry{modules: make(map[string]Module)}
}

func (r *Registry) Register(m Module) error {
	if m == nil {
		return fmt.Errorf("nil module")
	}
	id := strings.TrimSpace(m.ID())
	if id == "" {
		return fmt.Errorf("empty module id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.modules[id]; ok {
		return fmt.Errorf("module already registered: %s", id)
	}
	r.modules[id] = m
	return nil
}

func (r *Registry) MountAll(mux *http.ServeMux) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.modules {
		m.Mount(mux)
	}
}

func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.modules))
	for id := range r.modules {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
