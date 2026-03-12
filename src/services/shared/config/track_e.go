package config

import "fmt"

func RegisterTrackE(r *Registry) error {
	if r == nil {
		return fmt.Errorf("registry must not be nil")
	}

	entries := []Entry{
		{
			Key:         "limit.subagent_max_depth",
			Type:        TypeInt,
			Default:     "5",
			Description: "Sub-Agent 最大嵌套深度",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.subagent_max_active_per_root_run",
			Type:        TypeInt,
			Default:     "20",
			Description: "单 root run 下最大活跃 sub-agent 数量",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.subagent_max_parallel_children",
			Type:        TypeInt,
			Default:     "5",
			Description: "单 run 下最大并行子 agent 数量",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.subagent_max_descendants_per_root_run",
			Type:        TypeInt,
			Default:     "50",
			Description: "单 root run 下 sub-agent 总数上限",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.subagent_max_pending_per_root_run",
			Type:        TypeInt,
			Default:     "20",
			Description: "单 root run 下待处理输入队列上限",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
	}

	for _, e := range entries {
		if err := r.Register(e); err != nil {
			return err
		}
	}
	return nil
}
