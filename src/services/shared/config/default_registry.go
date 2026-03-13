package config

import "sync"

var (
	defaultRegistryOnce sync.Once
	defaultRegistry     *Registry
)

func DefaultRegistry() *Registry {
	defaultRegistryOnce.Do(func() {
		r := NewRegistry()
		if err := RegisterTrackA(r); err != nil {
			panic(err)
		}
		if err := RegisterTrackB(r); err != nil {
			panic(err)
		}
		if err := RegisterTrackE(r); err != nil {
			panic(err)
		}
		defaultRegistry = r
	})
	return defaultRegistry
}
