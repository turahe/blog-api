package storage

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/turahe/blog-api/internal/core/media/ports"
	"github.com/turahe/blog-api/internal/platform/config"
)

func TestClientImplementsObjectStorage(t *testing.T) {
	t.Parallel()

	var _ ports.ObjectStorage = (*Client)(nil)
}

func TestNewS3UsesPathStyleForCustomEndpoint(t *testing.T) {
	t.Parallel()

	client, err := NewS3(context.Background(), config.Config{
		S3Bucket:         "blog-media",
		S3Region:         "auto",
		S3AccessKey:      "minioadmin",
		S3SecretKey:      "minioadmin",
		S3Endpoint:       "http://127.0.0.1:9000",
		S3ForcePathStyle: false,
	})
	if err != nil {
		t.Fatalf("NewS3 returned error: %v", err)
	}

	if client.endpoint != "http://127.0.0.1:9000" {
		t.Fatalf("expected endpoint to be set, got %q", client.endpoint)
	}

	if !client.usePathStyle {
		t.Fatal("expected path-style requests for custom endpoint")
	}
}

func TestPresignPutReturnsSignedHeaders(t *testing.T) {
	t.Parallel()

	presigner := &fakePresignClient{
		request: &v4.PresignedHTTPRequest{
			URL: "https://example.test/upload",
			SignedHeader: http.Header{
				"Host":           []string{"example.test"},
				"Content-Type":   []string{"image/png"},
				"X-Amz-Meta-App": []string{"blog-api"},
			},
		},
	}
	client := &Client{
		bucket:  "blog-media",
		presign: presigner,
	}

	url, headers, err := client.PresignPut(context.Background(), "media/1/cover.png", "image/png", 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut returned error: %v", err)
	}

	if url != "https://example.test/upload" {
		t.Fatalf("expected URL to be preserved, got %q", url)
	}

	if headers["Content-Type"] != "image/png" {
		t.Fatalf("expected Content-Type header, got %#v", headers)
	}

	if headers["X-Amz-Meta-App"] != "blog-api" {
		t.Fatalf("expected signed metadata header, got %#v", headers)
	}

	if _, ok := headers["Host"]; ok {
		t.Fatalf("expected Host header to be omitted, got %#v", headers)
	}

	if presigner.lastExpires != 5*time.Minute {
		t.Fatalf("expected presign TTL to be forwarded, got %s", presigner.lastExpires)
	}
}

func TestHeadObjectMapsNotFound(t *testing.T) {
	t.Parallel()

	client := &Client{
		bucket: "blog-media",
		head: &fakeHeadClient{
			err: &smithy.GenericAPIError{
				Code:    "NotFound",
				Message: "missing",
			},
		},
	}

	_, err := client.HeadObject(context.Background(), "media/1/missing.png")
	if !errors.Is(err, ports.ErrObjectNotFound) {
		t.Fatalf("expected ports.ErrObjectNotFound, got %v", err)
	}
}

func TestHeadObjectReturnsObjectInfo(t *testing.T) {
	t.Parallel()

	client := &Client{
		bucket: "blog-media",
		head: &fakeHeadClient{
			output: &s3.HeadObjectOutput{
				ContentLength: aws.Int64(512),
				ContentType:   aws.String("image/webp"),
				ETag:          aws.String(`"etag-1"`),
			},
		},
	}

	info, err := client.HeadObject(context.Background(), "media/1/cover.webp")
	if err != nil {
		t.Fatalf("HeadObject returned error: %v", err)
	}

	if info.Size != 512 {
		t.Fatalf("expected size 512, got %d", info.Size)
	}

	if info.ContentType != "image/webp" {
		t.Fatalf("expected content type image/webp, got %q", info.ContentType)
	}

	if info.ETag != "etag-1" {
		t.Fatalf("expected trimmed etag, got %q", info.ETag)
	}
}

type fakeHeadClient struct {
	output *s3.HeadObjectOutput
	err    error
}

func (f *fakeHeadClient) HeadObject(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.output, nil
}

type fakePresignClient struct {
	request     *v4.PresignedHTTPRequest
	err         error
	lastExpires time.Duration
}

func (f *fakePresignClient) PresignPutObject(_ context.Context, _ *s3.PutObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	options := s3.PresignOptions{}
	for _, optFn := range optFns {
		optFn(&options)
	}

	f.lastExpires = options.Expires
	if f.err != nil {
		return nil, f.err
	}

	return f.request, nil
}
