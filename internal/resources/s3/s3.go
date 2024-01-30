package s3

import (
	"context"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awstypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/resources/types"
)

const Name = "S3" // type name for registry

var (
	_ types.ResourceProvider = &S3{} // compile time check that it implements interface
)

type Config struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket"`
	EndPoint  string `json:"endpoint"`
	Region    string `json:"region"`
	// For some s3-compatible object stores, converting the hostname is not required,
	// and not setting this option will result in not being able to access the corresponding object store address.
	// But Aliyun OSS should disable this option
	MutableHostname bool `json:"mutable_hostname"`
}

func New(config *Config) *S3 {
	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...any) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               config.EndPoint,
			SigningRegion:     config.Region,
			HostnameImmutable: !config.MutableHostname,
		}, nil
	})

	return &S3{
		bucket: config.Bucket,
		client: Lazy(func(ctx context.Context) (*awss3.Client, error) {
			awsConfig, err := s3config.LoadDefaultConfig(ctx,
				s3config.WithEndpointResolverWithOptions(resolver),
				s3config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.AccessKey, config.SecretKey, "")),
				s3config.WithRegion(config.Region),
			)
			if err != nil {
				return nil, errors.Wrapf(err, "load AWS config")
			}

			return awss3.NewFromConfig(awsConfig), nil
		}),
	}
}

type S3 struct {
	bucket string
	client *lazyInit[*awss3.Client]
}

func (s3 *S3) Upload(ctx context.Context, key string, payload io.Reader) error {
	client, err := s3.client.Get(ctx)
	if err != nil {
		return errors.Wrapf(err, "get AWS client")
	}
	uploader := manager.NewUploader(client)
	putInput := awss3.PutObjectInput{
		Bucket: aws.String(s3.bucket),
		Key:    aws.String(key),
		Body:   payload,
	}

	if _, err := uploader.Upload(ctx, &putInput); err != nil {
		return errors.Wrapf(err, "upload %q to S3", key)
	}
	return nil
}

func (s3 *S3) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	client, err := s3.client.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "get AWS client")
	}
	res, err := client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(s3.bucket),
		Key:    aws.String(key),
	})
	if isMissedKey(err) {
		err = types.ErrNotFound
	}
	if err != nil {
		return nil, errors.Wrapf(types.ErrNotFound, "get key %q", key)
	}
	return res.Body, nil
}

func (s3 *S3) Delete(ctx context.Context, key string) error {
	client, err := s3.client.Get(ctx)
	if err != nil {
		return errors.Wrapf(err, "get AWS client")
	}
	_, err = client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(s3.bucket),
		Key:    aws.String(key),
	})
	if isMissedKey(err) {
		err = nil
	}
	if err != nil {
		return errors.Wrapf(err, "delete key %q", key)
	}
	return nil
}

func isMissedKey(err error) bool {
	if err != nil {
		var nsk *awstypes.NoSuchKey
		if errors.As(err, &nsk) {
			return true
		}
	}
	return false
}

func Lazy[T any](handler func(ctx context.Context) (T, error)) *lazyInit[T] {
	return &lazyInit[T]{
		initializer: handler,
	}
}

type lazyInit[T any] struct {
	value       T
	ready       bool
	lock        sync.RWMutex
	initializer func(ctx context.Context) (T, error)
}

func (lz *lazyInit[T]) Get(ctx context.Context) (T, error) {
	// optimistic
	lz.lock.RLock()
	value, ready := lz.value, lz.ready
	lz.lock.RUnlock()
	if ready {
		return value, nil
	}
	// pessimistic
	lz.lock.Lock()
	defer lz.lock.Unlock()
	if lz.ready {
		return lz.value, nil
	}

	value, err := lz.initializer(ctx)
	if err != nil {
		return lz.value, err
	}
	lz.value = value
	lz.ready = true
	return value, nil
}
