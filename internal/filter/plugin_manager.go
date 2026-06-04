package filter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PluginManager loads and manages WASM plugins from a directory.
type PluginManager struct {
	dir     string
	mu      sync.RWMutex
	plugins map[string]*WASMFilter
}

// NewPluginManager creates a manager that watches dir for .wasm files.
func NewPluginManager(dir string) *PluginManager {
	return &PluginManager{
		dir:     dir,
		plugins: make(map[string]*WASMFilter),
	}
}

// LoadAll scans the plugin directory and loads all .wasm files.
// Existing plugins are replaced if the file has changed.
func (pm *PluginManager) LoadAll() error {
	entries, err := os.ReadDir(pm.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // directory missing is not fatal
		}
		return fmt.Errorf("read plugin dir: %w", err)
	}

	newPlugins := make(map[string]*WASMFilter)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".wasm") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".wasm")
		path := filepath.Join(pm.dir, entry.Name())

		filter, err := pm.loadPlugin(name, path)
		if err != nil {
			// Log and skip broken plugins so one bad file doesn't break the rest.
			continue
		}
		newPlugins[name] = filter
	}

	pm.mu.Lock()
	oldPlugins := pm.plugins
	pm.plugins = newPlugins
	pm.mu.Unlock()

	// Close old runtimes to free memory.
	for _, old := range oldPlugins {
		if old != nil {
			old.Close()
		}
	}
	return nil
}

// Get returns a loaded plugin by name.
func (pm *PluginManager) Get(name string) (*WASMFilter, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	f, ok := pm.plugins[name]
	return f, ok
}

// Names returns all loaded plugin names.
func (pm *PluginManager) Names() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}
	return names
}

// BuildRegistry registers all loaded plugins into a filter Registry.
func (pm *PluginManager) BuildRegistry() *Registry {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	registry := NewRegistry()
	for name, filter := range pm.plugins {
		f := filter // capture for closure
		_ = registry.Register(name, func() (Filter, error) {
			return f, nil
		})
	}
	return registry
}

// Close shuts down all plugin runtimes.
func (pm *PluginManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, f := range pm.plugins {
		if f != nil {
			f.Close()
		}
	}
	pm.plugins = nil
}

func (pm *PluginManager) loadPlugin(name, path string) (*WASMFilter, error) {
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	filter, err := NewWASMFilter(name, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", name, err)
	}
	return filter, nil
}

// AutoReload starts a background goroutine that reloads plugins every interval.
func (pm *PluginManager) AutoReload(interval time.Duration) func() {
	ticker := time.NewTicker(interval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				_ = pm.LoadAll()
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()
	return func() { close(done) }
}
