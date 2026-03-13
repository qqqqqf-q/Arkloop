//go:build !desktop

package pipeline

import "arkloop/services/worker/internal/data"

func defaultSkillResolver() SkillResolver {
	return data.SkillsRepository{}
}
