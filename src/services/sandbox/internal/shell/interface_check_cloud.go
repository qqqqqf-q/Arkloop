//go:build !desktop

package shell

import "arkloop/services/shared/objectstore"

var (
	_ stateStore    = (*objectstore.S3Store)(nil)
	_ artifactStore = (*objectstore.S3Store)(nil)
)
