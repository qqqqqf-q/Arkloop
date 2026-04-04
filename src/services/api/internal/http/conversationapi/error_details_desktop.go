//go:build desktop

package conversationapi

func internalErrorDetails(err error) map[string]any {
	if err == nil {
		return nil
	}
	return map[string]any{"reason": err.Error()}
}
