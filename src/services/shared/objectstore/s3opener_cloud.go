//go:build !desktop

package objectstore

func newS3BucketOpener(cfg S3Config) BucketOpener {
	return NewS3Opener(cfg)
}
