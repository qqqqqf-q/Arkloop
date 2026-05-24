//go:build darwin

package syncer

import "arkloop/services/activity-record/internal/sources/safari"

func init() {
	registerSourceBuilder("safari", func() (Source, error) {
		return safari.NewDefault()
	})
}
