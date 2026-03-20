package accountapi

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var channelRunTriggerLog sync.Mutex
var channelRunTriggerByChannel = map[uuid.UUID][]time.Time{}

func channelTelegramAgentTriggersPerMinute() int {
	const defaultPerMin = 30
	raw := strings.TrimSpace(os.Getenv("ARKLOOP_CHANNEL_RATE_LIMIT_PER_MIN"))
	if raw == "" {
		return defaultPerMin
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultPerMin
	}
	if n == 0 {
		return 0
	}
	return n
}

// channelAgentTriggerConsume 返回 true 表示允许本次创建 Agent Run；0 或负数环境值表示不限制。
func channelAgentTriggerConsume(channelID uuid.UUID) bool {
	if channelID == uuid.Nil {
		return true
	}
	per := channelTelegramAgentTriggersPerMinute()
	if per <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	channelRunTriggerLog.Lock()
	defer channelRunTriggerLog.Unlock()

	list := channelRunTriggerByChannel[channelID]
	var pruned []time.Time
	for _, ts := range list {
		if ts.After(cutoff) {
			pruned = append(pruned, ts)
		}
	}
	if len(pruned) >= per {
		channelRunTriggerByChannel[channelID] = pruned
		return false
	}
	pruned = append(pruned, now)
	channelRunTriggerByChannel[channelID] = pruned
	return true
}
