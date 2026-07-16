package datasources

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/truepace-io-oss/observability-mcp-server/internal/config"
)

// Registry is a thread-safe collection of the datasources this MCP instance manages.
type Registry struct {
	mu          sync.RWMutex
	datasources map[string]*Datasource
	order       []string
	defaultName string
}

// Build constructs a Registry from config. Building a datasource's client does
// not contact the backend, so an unreachable datasource does not fail Build;
// reachability is reported later via Datasource.Ping / the datasources_list tool.
func Build(cfg *config.Config) (*Registry, error) {
	r := &Registry{
		datasources: make(map[string]*Datasource, len(cfg.Datasources)),
		defaultName: cfg.DefaultDatasource,
	}
	for _, dc := range cfg.Datasources {
		ds, err := newDatasource(dc)
		if err != nil {
			return nil, err
		}
		r.datasources[dc.Name] = ds
		r.order = append(r.order, dc.Name)
	}
	if _, ok := r.datasources[r.defaultName]; !ok {
		return nil, fmt.Errorf("defaultDatasource %q not found after build", r.defaultName)
	}
	return r, nil
}

// NewRegistryForTest assembles a Registry from pre-built datasources.
func NewRegistryForTest(defaultName string, dss ...*Datasource) *Registry {
	r := &Registry{datasources: make(map[string]*Datasource, len(dss)), defaultName: defaultName}
	for _, d := range dss {
		r.datasources[d.Name] = d
		r.order = append(r.order, d.Name)
	}
	return r
}

// Get returns the named datasource, or the default when name is empty. It
// returns a descriptive error (listing valid names) on a miss.
func (r *Registry) Get(name string) (*Datasource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name == "" {
		name = r.defaultName
	}
	ds, ok := r.datasources[name]
	if !ok {
		return nil, fmt.Errorf("unknown datasource %q; configured datasources: %s", name, strings.Join(r.namesLocked(), ", "))
	}
	return ds, nil
}

// Default returns the default datasource.
func (r *Registry) Default() *Datasource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.datasources[r.defaultName]
}

// DefaultName returns the configured default datasource name.
func (r *Registry) DefaultName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultName
}

// Names returns the datasource names in configuration order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.namesLocked()
}

func (r *Registry) namesLocked() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// All returns every datasource sorted by name (stable output for datasources_list).
func (r *Registry) All() []*Datasource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := r.namesLocked()
	sort.Strings(names)
	out := make([]*Datasource, 0, len(names))
	for _, n := range names {
		out = append(out, r.datasources[n])
	}
	return out
}
