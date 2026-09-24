// Package storage implements media object storage on S3-compatible services.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/turahe/blog-api/internal/core/media/ports"
	appconfig "github.com/turahe/blog-api/internal/platform/config"
)

type headObjectClient interface {
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

type presignPutClient interface {
	PresignPutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

type putObjectClient interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type getObjectClient interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// Client implements mediaports.ObjectStorage on an S3-compatible bucket.
type Client struct {
	bucket       string
	endpoint     string
	usePathStyle bool
	head         headObjectClient
	presign      presignPutClient
	put          putObjectClient
	get          getObjectClient
}

var _ ports.ObjectStorage = (*Client)(nil)

// NewS3 builds a client from the S3_* settings (path-style addressing for MinIO/RustFS).
func NewS3(ctx context.Context, cfg appconfig.Config) (*Client, error) {
	bucket := strings.TrimSpace(cfg.S3Bucket)
	if bucket == "" {
		return nil, errors.New("S3_BUCKET must not be empty")
	}

	if strings.TrimSpace(cfg.S3AccessKey) == "" {
		return nil, errors.New("S3_ACCESS_KEY must not be empty")
	}

	if strings.TrimSpace(cfg.S3SecretKey) == "" {
		return nil, errors.New("S3_SECRET_KEY must not be empty")
	}

	region := strings.TrimSpace(cfg.S3Region)
	if region == "" {
		region = "auto"
	}

	endpoint := strings.TrimSpace(cfg.S3Endpoint)
	usePathStyle := cfg.S3ForcePathStyle || endpoint != ""

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.S3AccessKey,
			cfg.S3SecretKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = usePathStyle
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	return &Client{
		bucket:       bucket,
		endpoint:     endpoint,
		usePathStyle: usePathStyle,
		head:         s3Client,
		presign:      s3.NewPresignClient(s3Client),
		put:          s3Client,
		get:          s3Client,
	}, nil
}

// ReadPrefix fetches at most n leading bytes with a ranged GET, or ports.ErrObjectNotFound.
func (c *Client) ReadPrefix(ctx context.Context, key string, n int64) ([]byte, error) {
	if n < 1 {
		return nil, fmt.Errorf("read prefix: invalid length %d", n)
	}

	output, err := c.get.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=0-%d", n-1)),
	})
	if err != nil {
		if isObjectNotFound(err) {
			return nil, fmt.Errorf("%w: bucket=%s key=%s", ports.ErrObjectNotFound, c.bucket, key)
		}

		return nil, err
	}
	defer func() { _ = output.Body.Close() }()

	return io.ReadAll(io.LimitReader(output.Body, n))
}

// PutObject uploads body under key with the given content type.
func (c *Client) PutObject(ctx context.Context, key, contentType string, body []byte) error {
	_, err := c.put.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String(contentType),
	})

	return err
}

// PresignPut returns a presigned PUT URL and the headers the uploader must send.
func (c *Client) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, map[string]string, error) {
	input := &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}
	if contentType = strings.TrimSpace(contentType); contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	request, err := c.presign.PresignPutObject(ctx, input, func(o *s3.PresignOptions) {
		o.Expires = ttl
	})
	if err != nil {
		return "", nil, err
	}

	headers := signedHeaders(request.SignedHeader)
	if contentType != "" {
		headers["Content-Type"] = contentType
	}

	return request.URL, headers, nil
}

// HeadObject returns object metadata, or ports.ErrObjectNotFound when the key is missing.
func (c *Client) HeadObject(ctx context.Context, key string) (ports.ObjectInfo, error) {
	output, err := c.head.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isObjectNotFound(err) {
			return ports.ObjectInfo{}, fmt.Errorf("%w: bucket=%s key=%s", ports.ErrObjectNotFound, c.bucket, key)
		}

		return ports.ObjectInfo{}, err
	}

	return ports.ObjectInfo{
		Size:        aws.ToInt64(output.ContentLength),
		ContentType: strings.TrimSpace(aws.ToString(output.ContentType)),
		ETag:        strings.Trim(aws.ToString(output.ETag), `"`),
	}, nil
}

func signedHeaders(header http.Header) map[string]string {
	if len(header) == 0 {
		return map[string]string{}
	}

	headers := make(map[string]string, len(header))
	for key, values := range header {
		if strings.EqualFold(key, "host") || len(values) == 0 {
			continue
		}

		headers[key] = strings.Join(values, ", ")
	}

	return headers
}

func isObjectNotFound(err error) bool {
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "404":
			return true
		}
	}

	var statusErr interface{ HTTPStatusCode() int }
	if errors.As(err, &statusErr) && statusErr.HTTPStatusCode() == http.StatusNotFound {
		return true
	}

	return false
}
