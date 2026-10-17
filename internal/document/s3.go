package document

import (
	"ai-business-api/internal/customer"
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3API interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}
type S3Store struct {
	Client S3API
	Bucket string
}

func objectKey(org, id string) (string, error) {
	if !customer.UUID(org) || !customer.UUID(id) {
		return "", errors.New("invalid storage key")
	}
	return "documents/" + org + "/" + id, nil
}
func (s S3Store) Put(ctx context.Context, org, id string, data []byte) error {
	key, err := objectKey(org, id)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > MaxSize {
		return ErrSize
	}
	// Conditional writes preserve originals even when the provider retries a request.
	_, err = s.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key), Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))), ContentType: aws.String("application/octet-stream"), IfNoneMatch: aws.String("*")})
	return err
}
func (s S3Store) Get(ctx context.Context, org, id string) ([]byte, error) {
	key, err := objectKey(org, id)
	if err != nil {
		return nil, err
	}
	result, err := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	if result.ContentLength != nil && *result.ContentLength > MaxSize {
		return nil, ErrSize
	}
	data, err := io.ReadAll(io.LimitReader(result.Body, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSize {
		return nil, ErrSize
	}
	return data, nil
}

// CheckHealth checks bucket access without reading or creating tenant objects.
func (s S3Store) CheckHealth(ctx context.Context) error {
	client, ok := s.Client.(interface {
		HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	})
	if !ok {
		return ErrStorageUnavailable
	}
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.Bucket)})
	return err
}
