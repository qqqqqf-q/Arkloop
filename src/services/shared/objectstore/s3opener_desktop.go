//go:build desktop

package objectstore

import (
"context"
"fmt"
)

func newS3BucketOpener(_ S3Config) BucketOpener {
return errOpener("S3 backend is not available in desktop mode")
}

type errOpener string

func (e errOpener) Open(_ context.Context, _ string) (Store, error) {
return nil, fmt.Errorf("%s", string(e))
}
