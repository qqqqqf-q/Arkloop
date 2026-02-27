package template

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Template 描述一个 microVM 模板：rootfs + kernel + tier 的组合。
// 例：python3.12-lite = python3.12.ext4 + vmlinux + lite tier
type Template struct {
	ID              string   `json:"id"`
	KernelImagePath string   `json:"kernel_image_path"`
	RootfsPath      string   `json:"rootfs_path"`
	Tier            string   `json:"tier"` // "lite" | "pro" | "ultra"
	Languages       []string `json:"languages"`
}

// Registry 持有所有已注册的 template，按 ID 索引。
type Registry struct {
	templates map[string]Template
}

// LoadFromFile 从 JSON 文件加载 template 列表，返回 Registry。
func LoadFromFile(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read templates file %q: %w", path, err)
	}

	var list []Template
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse templates file: %w", err)
	}

	r := &Registry{templates: make(map[string]Template, len(list))}
	for _, t := range list {
		t.ID = strings.TrimSpace(t.ID)
		if t.ID == "" {
			return nil, fmt.Errorf("template missing id: %+v", t)
		}
		if _, dup := r.templates[t.ID]; dup {
			return nil, fmt.Errorf("duplicate template id: %q", t.ID)
		}
		r.templates[t.ID] = t
	}
	return r, nil
}

// Get 按 ID 返回 template。
func (r *Registry) Get(id string) (Template, bool) {
	t, ok := r.templates[id]
	return t, ok
}

// All 返回所有注册的 template 列表（顺序不保证）。
func (r *Registry) All() []Template {
	result := make([]Template, 0, len(r.templates))
	for _, t := range r.templates {
		result = append(result, t)
	}
	return result
}

// ForTier 返回匹配指定 tier 的第一个 template。
// 命名惯例：{lang}-{tier}，优先匹配 python3.12-{tier}。
func (r *Registry) ForTier(tier string) (Template, bool) {
	// 优先精确匹配 python3.12-{tier}
	if t, ok := r.templates["python3.12-"+tier]; ok {
		return t, true
	}
	// 回退：找任意匹配 tier 的 template
	for _, t := range r.templates {
		if t.Tier == tier {
			return t, true
		}
	}
	return Template{}, false
}
