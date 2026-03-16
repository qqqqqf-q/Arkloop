//go:build !desktop

package skills

import "arkloop/services/shared/objectstore"

var _ Store = (*objectstore.S3Store)(nil)
