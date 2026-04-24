//go:build !desktop

package objectstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3Store 封装 S3 兼容存储客户端，绑定到单个 bucket。
// 支持 MinIO、AWS S3、GCS（S3 兼容模式）等。
type S3Store struct {
	client *s3.Client
	bucket string
	region string
}

type S3Opener struct {
	config S3Config
}

func New(ctx context.Context, endpoint, accessKey, secretKey, bucket, region string) (Store, error) {
	return NewS3Opener(S3Config{
		Endpoint:  endpoint,
		AccessKey: accessKey,
		SecretKey: secretKey,
		Region:    region,
	}).Open(ctx, bucket)
}

func NewS3Opener(cfg S3Config) *S3Opener {
	return &S3Opener{config: normalizeS3Config(cfg)}
}

func (o *S3Opener) Open(ctx context.Context, bucket string) (Store, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := normalizeS3Config(o.config)
	bucket = strings.TrimSpace(bucket)
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("s3 endpoint must not be empty")
	}
	if cfg.AccessKey == "" {
		return nil, fmt.Errorf("s3 access key must not be empty")
	}
	if cfg.SecretKey == "" {
		return nil, fmt.Errorf("s3 secret key must not be empty")
	}
	if bucket == "" {
		return nil, fmt.Errorf("s3 bucket must not be empty")
	}

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
		options.UsePathStyle = true
	})

	store := &S3Store{client: client, bucket: bucket, region: cfg.Region}
	if err := store.ensureBucket(ctx); err != nil {
		return nil, fmt.Errorf("ensure bucket %q: %w", bucket, err)
	}
	return store, nil
}

func (o *S3Store) ensureBucket(ctx context.Context) error {
	_, err := o.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(o.bucket)})
	if err == nil {
		return nil
	}

	var notFound *types.NotFound
	if !errors.As(err, &notFound) {
		return fmt.Errorf("head bucket: %w", err)
	}

	createInput := &s3.CreateBucketInput{Bucket: aws.String(o.bucket)}
	if o.region != "" && o.region != "us-east-1" {
		createInput.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(o.region),
		}
	}

	_, createErr := o.client.CreateBucket(ctx, createInput)
	if createErr != nil {
		var alreadyOwned *types.BucketAlreadyOwnedByYou
		var alreadyExists *types.BucketAlreadyExists
		if errors.As(createErr, &alreadyOwned) || errors.As(createErr, &alreadyExists) {
			return nil
		}
		return fmt.Errorf("create bucket: %w", createErr)
	}
	return nil
}

func (o *S3Store) Put(ctx context.Context, key string, data []byte) error {
	return o.PutObject(ctx, key, data, PutOptions{})
}

func (o *S3Store) PutIfAbsent(ctx context.Context, key string, data []byte) (bool, error) {
	if _, err := o.Head(ctx, key); err == nil {
		return false, nil
	} else if !IsNotFound(err) {
		return false, err
	}
	if err := o.Put(ctx, key, data); err != nil {
		return false, err
	}
	return true, nil
}

func (o *S3Store) PutWithContentType(ctx context.Context, key string, data []byte, contentType string) error {
	return o.PutObject(ctx, key, data, PutOptions{ContentType: contentType})
}

func (o *S3Store) PutObject(ctx context.Context, key string, data []byte, options PutOptions) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(o.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	}
	if options.ContentType != "" {
		input.ContentType = aws.String(options.ContentType)
	}
	if metadata := normalizeMetadata(options.Metadata); len(metadata) > 0 {
		input.Metadata = metadata
	}
	_, err := o.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
}

func (o *S3Store) Head(ctx context.Context, key string) (ObjectInfo, error) {
	out, err := o.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(o.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("head object %q: %w", key, err)
	}
	return ObjectInfo{
		Key:         key,
		ContentType: aws.ToString(out.ContentType),
		Metadata:    normalizeMetadata(out.Metadata),
		Size:        aws.ToInt64(out.ContentLength),
		ETag:        strings.Trim(aws.ToString(out.ETag), `"`),
	}, nil
}

func (o *S3Store) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := o.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(o.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %q: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("read object %q: %w", key, err)
	}
	return data, nil
}

func (o *S3Store) GetWithContentType(ctx context.Context, key string) ([]byte, string, error) {
	out, err := o.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(o.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", fmt.Errorf("get object %q: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()

	contentType := aws.ToString(out.ContentType)
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read object %q: %w", key, err)
	}
	return data, contentType, nil
}

func (o *S3Store) SetLifecycleExpirationDays(ctx context.Context, days int) error {
	if days <= 0 {
		return nil
	}
	config := expirationLifecycleConfiguration(days)
	_, err := o.client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket:                 aws.String(o.bucket),
		LifecycleConfiguration: config,
	})
	if err != nil {
		return fmt.Errorf("put lifecycle configuration for bucket %q: %w", o.bucket, err)
	}
	return nil
}

func expirationLifecycleConfiguration(days int) *types.BucketLifecycleConfiguration {
	return &types.BucketLifecycleConfiguration{
		Rules: []types.LifecycleRule{{
			ID:     aws.String("expire-after-days"),
			Status: types.ExpirationStatusEnabled,
			Filter: &types.LifecycleRuleFilter{Prefix: aws.String("")},
			Expiration: &types.LifecycleExpiration{
				Days: aws.Int32(int32(days)),
			},
		}},
	}
}

func (o *S3Store) Delete(ctx context.Context, key string) error {
	_, err := o.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(o.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object %q: %w", key, err)
	}
	return nil
}

func (o *S3Store) ListPrefix(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	paginator := s3.NewListObjectsV2Paginator(o.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(o.bucket),
		Prefix: aws.String(strings.TrimSpace(prefix)),
	})

	objects := make([]ObjectInfo, 0)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list objects with prefix %q: %w", prefix, err)
		}
		for _, item := range page.Contents {
			objects = append(objects, ObjectInfo{
				Key:  strings.TrimSpace(aws.ToString(item.Key)),
				Size: aws.ToInt64(item.Size),
				ETag: strings.Trim(aws.ToString(item.ETag), `"`),
			})
		}
	}
	return objects, nil
}

func (o *S3Store) WriteJSONAtomic(ctx context.Context, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal json %q: %w", key, err)
	}
	return o.PutObject(ctx, key, payload, PutOptions{ContentType: "application/json"})
}

// IsNotFound reports whether err represents a "not found" condition from S3 or
// the local filesystem.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := strings.TrimSpace(apiErr.ErrorCode())
		switch code {
		case "NoSuchKey", "NotFound", "NoSuchBucket", "404":
			return true
		}
	}
	var notFound *types.NotFound
	return errors.As(err, &notFound)
}

var _ Store = (*S3Store)(nil)
var _ BlobStore = (*S3Store)(nil)
var _ LifecycleConfigurator = (*S3Store)(nil)
var _ BucketOpener = (*S3Opener)(nil)
