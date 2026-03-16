package adminapi

import (
	"regexp"
)

var uuidPrefixRegex = regexp.MustCompile(`^[0-9a-fA-F-]{1,36}$`)

func calcCacheHitRate(inputTokens, cacheRead, cacheCreation, cachedTokens *int64) *float64 {
	hasAnthropic := (cacheRead != nil && *cacheRead > 0) || (cacheCreation != nil && *cacheCreation > 0)
	hasOpenAI := cachedTokens != nil && *cachedTokens > 0

	if hasAnthropic && hasOpenAI {
		return nil
	}
	if hasAnthropic {
		total := 0.0
		if inputTokens != nil {
			total += float64(*inputTokens)
		}
		if cacheRead != nil {
			total += float64(*cacheRead)
		}
		if cacheCreation != nil {
			total += float64(*cacheCreation)
		}
		if total <= 0 {
			return nil
		}
		read := 0.0
		if cacheRead != nil {
			read = float64(*cacheRead)
		}
		r := read / total
		return &r
	}
	if hasOpenAI && inputTokens != nil && *inputTokens > 0 {
		r := float64(*cachedTokens) / float64(*inputTokens)
		return &r
	}
	return nil
}
