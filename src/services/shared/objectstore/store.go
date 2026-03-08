package objectstore

import (
	"bytes"
	"context"
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

const ArtifactBucket = "sandbox-artifacts"
const SessionStateBucket = "sandbox-session-state"
const EnvironmentStateBucket = "sandbox-environments"

// Store 封装 S3 兼容存储客户端，绑定到单个 bucket。
// 支持 MinIO、AWS S3、GCS（S3 兼容模式）等。
type Store struct {
	client *s3.Client
	bucket string
	region string
}

type PutOptions struct {
	ContentType string
	Metadata    map[string]string
}

type ObjectInfo struct {
	Key         string
	ContentType string
	Metadata    map[string]string
	Size        int64
}

// New 初始化 S3 客户端，并确保目标 bucket 存在。
// region 为空时默认 "us-east-1"（兼容 MinIO）。
func New(ctx context.Context, endpoint, accessKey, secretKey, bucket, region string) (*Store, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	endpoint = strings.TrimSpace(endpoint)
	accessKey = strings.TrimSpace(accessKey)
	secretKey = strings.TrimSpace(secretKey)
	bucket = strings.TrimSpace(bucket)
	region = strings.TrimSpace(region)

	if endpoint == "" {
		return nil, fmt.Errorf("s3 endpoint must not be empty")
	}
	if accessKey == "" {
		return nil, fmt.Errorf("s3 access key must not be empty")
	}
	if secretKey == "" {
		return nil, fmt.Errorf("s3 secret key must not be empty")
	}
	if bucket == "" {
		return nil, fmt.Errorf("s3 bucket must not be empty")
	}
	if region == "" {
		region = "us-east-1"
	}

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true // MinIO 要求 path-style 寻址
	})

	store := &Store{client: client, bucket: bucket, region: region}

	if err := store.ensureBucket(ctx); err != nil {
		return nil, fmt.Errorf("ensure bucket %q: %w", bucket, err)
	}

	return store, nil
}

// ensureBucket 如果 bucket 不存在则创建。
func (o *Store) ensureBucket(ctx context.Context) error {
	_, err := o.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(o.bucket),
	})
	if err == nil {
		return nil
	}

	// 判断是否是 bucket 不存在（404）
	var notFound *types.NotFound
	if !errors.As(err, &notFound) {
		// 非 404 错误（如网络问题、权限问题），直接返回
		return fmt.Errorf("head bucket: %w", err)
	}

	// Bucket 不存在，创建它
	createInput := &s3.CreateBucketInput{
		Bucket: aws.String(o.bucket),
	}
	// AWS S3 非 us-east-1 区域需要指定 LocationConstraint
	if o.region != "" && o.region != "us-east-1" {
		createInput.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(o.region),
		}
	}

	_, createErr := o.client.CreateBucket(ctx, createInput)
	if createErr != nil {
		// 并发场景下 bucket 可能已被创建
		var alreadyOwned *types.BucketAlreadyOwnedByYou
		var alreadyExists *types.BucketAlreadyExists
		if errors.As(createErr, &alreadyOwned) || errors.As(createErr, &alreadyExists) {
			return nil
		}
		return fmt.Errorf("create bucket: %w", createErr)
	}

	return nil
}

// Put 上传对象，key 为对象路径。
func (o *Store) Put(ctx context.Context, key string, data []byte) error {
	return o.PutObject(ctx, key, data, PutOptions{})
}

// PutWithContentType 上传对象并指定 Content-Type。
func (o *Store) PutWithContentType(ctx context.Context, key string, data []byte, contentType string) error {
	return o.PutObject(ctx, key, data, PutOptions{ContentType: contentType})
}

func (o *Store) PutObject(ctx context.Context, key string, data []byte, options PutOptions) error {
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

func (o *Store) Head(ctx context.Context, key string) (ObjectInfo, error) {
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
	}, nil
}

// Get 下载对象并返回内容。
func (o *Store) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := o.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(o.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %q: %w", key, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("read object %q: %w", key, err)
	}
	return data, nil
}

// GetWithContentType 下载对象并返回内容及 Content-Type。
func (o *Store) GetWithContentType(ctx context.Context, key string) ([]byte, string, error) {
	out, err := o.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(o.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", fmt.Errorf("get object %q: %w", key, err)
	}
	defer out.Body.Close()

	contentType := aws.ToString(out.ContentType)

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read object %q: %w", key, err)
	}
	return data, contentType, nil
}

func normalizeMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	cleaned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		cleaned[normalizedKey] = strings.TrimSpace(value)
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func (o *Store) SetLifecycleExpirationDays(ctx context.Context, days int) error {
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
		Rules: []types.LifecycleRule{
			{
				ID:     aws.String("expire-after-days"),
				Status: types.ExpirationStatusEnabled,
				Filter: &types.LifecycleRuleFilter{Prefix: aws.String("")},
				Expiration: &types.LifecycleExpiration{
					Days: aws.Int32(int32(days)),
				},
			},
		},
	}
}

// Delete 删除对象。
func (o *Store) Delete(ctx context.Context, key string) error {
	_, err := o.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(o.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object %q: %w", key, err)
	}
	return nil
}

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
