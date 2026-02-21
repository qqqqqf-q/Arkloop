package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Store 封装 S3 兼容存储客户端，绑定到单个 bucket。
// 支持 MinIO、AWS S3、GCS（S3 兼容模式）等。
type Store struct {
	client *s3.Client
	bucket string
	region string
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
	_, err := o.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(o.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
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
