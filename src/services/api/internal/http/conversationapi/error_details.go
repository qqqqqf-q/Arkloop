//go:build !desktop

package conversationapi

func internalErrorDetails(err error) map[string]any {
	return nil
}
