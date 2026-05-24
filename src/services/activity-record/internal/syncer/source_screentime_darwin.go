//go:build darwin

package syncer

import "arkloop/services/activity-record/internal/sources/screentime"

func init() {
	registerSourceBuilder("screentime", func() (Source, error) {
		return screentime.NewDefault()
	})
}
