package config

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const (
	TypeString   = "string"
	TypeInt      = "int"
	TypeNumber   = "number"
	TypeBool     = "bool"
	TypeDuration = "duration"
)

const (
	ScopePlatform = "platform"
	ScopeProject  = "project"
	ScopeBoth     = "both"
)

type Entry struct {
	Key         string
	Type        string
	Default     string
	Description string
	Sensitive   bool
	Scope       string

	// EnvKeys 为空时自动推导：ARKLOOP_{UPPER(SNAKE(key))}
	EnvKeys []string
}

type Registry struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

func NewRegistry() *Registry {
	return &Registry{entries: map[string]Entry{}}
}

func (r *Registry) Register(e Entry) error {
	if r == nil {
		return fmt.Errorf("registry must not be nil")
	}

	cleanKey := strings.TrimSpace(e.Key)
	if cleanKey == "" {
		return fmt.Errorf("config entry key must not be empty")
	}
	if strings.IndexFunc(cleanKey, unicode.IsSpace) >= 0 {
		return fmt.Errorf("config entry key %q must not contain whitespace", cleanKey)
	}
	e.Key = cleanKey

	switch e.Type {
	case TypeString, TypeInt, TypeNumber, TypeBool, TypeDuration:
	default:
		return fmt.Errorf("config entry %q: unsupported type %q", e.Key, e.Type)
	}

	switch e.Scope {
	case ScopePlatform, ScopeProject, ScopeBoth:
	default:
		return fmt.Errorf("config entry %q: unsupported scope %q", e.Key, e.Scope)
	}

	e.EnvKeys = normalizeEnvKeys(e.EnvKeys)

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.entries[e.Key]; ok {
		if equalEntry(existing, e) {
			return nil
		}
		return fmt.Errorf("config entry %q already registered with different metadata", e.Key)
	}

	r.entries[e.Key] = e
	return nil
}

func (r *Registry) Get(key string) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[key]
	return e, ok
}

func (r *Registry) List() []Entry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	keys := make([]string, 0, len(r.entries))
	for k := range r.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]Entry, 0, len(keys))
	for _, k := range keys {
		out = append(out, r.entries[k])
	}
	return out
}

func (r *Registry) ListByPrefix(prefix string) []Entry {
	cleaned := strings.TrimSpace(prefix)
	if cleaned == "" {
		return nil
	}

	items := r.List()
	out := make([]Entry, 0, len(items))
	for _, e := range items {
		if strings.HasPrefix(e.Key, cleaned) {
			out = append(out, e)
		}
	}
	return out
}

func normalizeEnvKeys(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		k := strings.TrimSpace(item)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func equalEntry(a, b Entry) bool {
	if a.Key != b.Key ||
		a.Type != b.Type ||
		a.Default != b.Default ||
		a.Description != b.Description ||
		a.Sensitive != b.Sensitive ||
		a.Scope != b.Scope {
		return false
	}
	if len(a.EnvKeys) != len(b.EnvKeys) {
		return false
	}
	for i := range a.EnvKeys {
		if a.EnvKeys[i] != b.EnvKeys[i] {
			return false
		}
	}
	return true
}
