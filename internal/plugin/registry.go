package plugin

import (
	"fmt"
	"sync"
)

type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

var global = &Registry{plugins: make(map[string]Plugin)}

// NewRegistry returns a new empty plugin registry. Use this in tests so that
// each test gets an isolated registry instead of sharing the global singleton.
func NewRegistry() *Registry { return &Registry{plugins: make(map[string]Plugin)} }

func Register(p Plugin) {
	global.mu.Lock()
	defer global.mu.Unlock()
	if _, exists := global.plugins[p.Name()]; exists {
		panic(fmt.Sprintf("plugin %q already registered", p.Name()))
	}
	global.plugins[p.Name()] = p
}

func Global() *Registry { return global }

func (r *Registry) All() map[string]Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]Plugin, len(r.plugins))
	for k, v := range r.plugins {
		out[k] = v
	}
	return out
}

// lookup returns the plugin for name. Explicit registrations take precedence;
// names matching "mise-<tool>" are synthesised on the fly. Must be called with
// r.mu held (at least for reading).
func (r *Registry) lookup(name string) (Plugin, bool) {
	if p, ok := r.plugins[name]; ok {
		return p, ok
	}
	return NewMisePlugin(name)
}

func (r *Registry) Get(name string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lookup(name)
}

// Set registers or replaces a plugin by name (upsert).
func (r *Registry) Set(p Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.Name()] = p
}

func (r *Registry) Resolve(enabled []string) ([]Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	needed := make(map[string]bool)
	var expand func(name string) error
	expand = func(name string) error {
		if needed[name] {
			return nil
		}
		needed[name] = true
		p, ok := r.lookup(name)
		if !ok {
			return fmt.Errorf("plugin %q not found (check spelling or register it)", name)
		}
		for _, dep := range p.Dependencies() {
			if err := expand(dep); err != nil {
				return err
			}
		}
		return nil
	}
	for _, name := range enabled {
		if err := expand(name); err != nil {
			return nil, err
		}
	}

	subgraph := make(map[string][]string, len(needed))
	for name := range needed {
		p, _ := r.lookup(name)
		var deps []string
		for _, dep := range p.Dependencies() {
			if needed[dep] {
				deps = append(deps, dep)
			}
		}
		subgraph[name] = deps
	}

	order, err := topologicalSort(subgraph)
	if err != nil {
		return nil, err
	}

	result := make([]Plugin, 0, len(order))
	for _, name := range order {
		p, _ := r.lookup(name)
		result = append(result, p)
	}
	return result, nil
}
