package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestReadPrefixRequestsRange(t *testing.T) {
	t.Parallel()

	getter := &fakeGetClient{body: "0123456789"}
	client := &Client{bucket: "blog-media", get: getter}

	got, err := client.ReadPrefix(context.Background(), "media/1/a.png", 4)
	if err != nil {
		t.Fatalf("ReadPrefix returned error: %v", err)
	}

	if string(got) != "0123" {
		t.Fatalf("expected body capped at 4 bytes, got %q", got)
	}

	if aws.ToString(getter.lastRange) != "bytes=0-3" {
		t.Fatalf("expected range bytes=0-3, got %q", aws.ToString(getter.lastRange))
	}
}

func TestReadPrefixMapsNotFound(t *testing.T) {
	t.Parallel()

	client := &Client{
		bucket: "blog-media",
		get:    &fakeGetClient{err: &smithy.GenericAPIError{Code: "NoSuchKey", Message: "missing"}},
	}

	_, err := client.ReadPrefix(context.Background(), "media/1/missing.png", 512)
	if !errors.Is(err, ports.ErrObjectNotFound) {
		t.Fatalf("expected ports.ErrObjectNotFound, got %v", err)
	}
}

type fakeGetClient struct {
	body      string
	err       error
	lastRange *string
}

func (f *fakeGetClient) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.lastRange = params.Range
	if f.err != nil {
		return nil, f.err
	}

	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(f.body))}, nil
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

func (f *fakePresignClient) PresignGetObject(_ context.Context, _ *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
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

type fakeDeleteClient struct {
	err     error
	deleted []string
}

func (f *fakeDeleteClient) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.deleted = append(f.deleted, aws.ToString(params.Key))
	return &s3.DeleteObjectOutput{}, f.err
}

func TestPresignGetUsesTTL(t *testing.T) {
	t.Parallel()

	presigner := &fakePresignClient{request: &v4.PresignedHTTPRequest{URL: "https://s3.test/bucket/key?sig"}}
	client := &Client{bucket: "bucket", presignGet: presigner}

	url, err := client.PresignGet(t.Context(), "privacy-exports/a.json", 15*time.Minute)
	if err != nil {
		t.Fatalf("presign get: %v", err)
	}

	if url != "https://s3.test/bucket/key?sig" || presigner.lastExpires != 15*time.Minute {
		t.Fatalf("got url %q expires %s", url, presigner.lastExpires)
	}
}

func TestDeleteObjectIgnoresMissingKeys(t *testing.T) {
	t.Parallel()

	deleter := &fakeDeleteClient{err: &smithy.GenericAPIError{Code: "NoSuchKey", Message: "missing"}}
	client := &Client{bucket: "bucket", del: deleter}

	if err := client.DeleteObject(t.Context(), "gone"); err != nil {
		t.Fatalf("missing key should not fail: %v", err)
	}

	if len(deleter.deleted) != 1 || deleter.deleted[0] != "gone" {
		t.Fatalf("deleted %v", deleter.deleted)
	}

	deleter.err = errors.New("boom")
	if err := client.DeleteObject(t.Context(), "key"); err == nil {
		t.Fatal("expected storage errors to surface")
	}
}

func TestNewS3RejectsMissingSettings(t *testing.T) {
	t.Parallel()

	valid := config.Config{S3Bucket: "blog-media", S3AccessKey: "key", S3SecretKey: "secret"}

	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr string
	}{
		{name: "blank bucket", mutate: func(c *config.Config) { c.S3Bucket = "  " }, wantErr: "S3_BUCKET"},
		{name: "blank access key", mutate: func(c *config.Config) { c.S3AccessKey = "" }, wantErr: "S3_ACCESS_KEY"},
		{name: "blank secret key", mutate: func(c *config.Config) { c.S3SecretKey = " " }, wantErr: "S3_SECRET_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			tt.mutate(&cfg)

			client, err := NewS3(t.Context(), cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Nil(t, client)
		})
	}
}

func TestNewS3DefaultsRegionAndVirtualHostStyle(t *testing.T) {
	t.Parallel()

	client, err := NewS3(t.Context(), config.Config{S3Bucket: " blog-media ", S3AccessKey: "key", S3SecretKey: "secret"})
	require.NoError(t, err)

	assert.Equal(t, "blog-media", client.bucket)
	assert.False(t, client.usePathStyle)

	s3Client, ok := client.head.(*s3.Client)
	require.True(t, ok)
	assert.Equal(t, "auto", s3Client.Options().Region)
}

func TestNewS3ReportsConfigLoadFailure(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))

	t.Setenv("AWS_CONFIG_FILE", empty)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", empty)
	t.Setenv("AWS_PROFILE", "profile-that-does-not-exist")

	_, err := NewS3(t.Context(), config.Config{S3Bucket: "b", S3AccessKey: "k", S3SecretKey: "s"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load aws config")
}

func TestReadPrefixRejectsInvalidLength(t *testing.T) {
	t.Parallel()

	for _, n := range []int64{0, -1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Parallel()

			getter := &fakeGetClient{body: "data"}
			client := &Client{bucket: "blog-media", get: getter}

			_, err := client.ReadPrefix(t.Context(), "k", n)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid length")
			assert.Nil(t, getter.lastRange, "no request should be issued")
		})
	}
}

func TestReadPrefixSurfacesStorageErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	client := &Client{bucket: "blog-media", get: &fakeGetClient{err: boom}}

	_, err := client.ReadPrefix(t.Context(), "k", 8)
	require.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, ports.ErrObjectNotFound)
}

func TestPutObjectUploadsThroughEndpoint(t *testing.T) {
	t.Parallel()

	type captured struct {
		method, path, contentType string
		body                      []byte
	}

	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "success", status: http.StatusOK},
		{name: "access denied", status: http.StatusForbidden, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := make(chan captured, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				got <- captured{method: r.Method, path: r.URL.Path, contentType: r.Header.Get("Content-Type"), body: body}

				if tt.status != http.StatusOK {
					w.Header().Set("Content-Type", "application/xml")
					w.WriteHeader(tt.status)
					_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`)

					return
				}

				w.Header().Set("ETag", `"etag"`)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			client, err := NewS3(t.Context(), config.Config{
				S3Bucket:    "blog-media",
				S3Region:    "us-east-1",
				S3AccessKey: "key",
				S3SecretKey: "secret",
				S3Endpoint:  srv.URL,
			})
			require.NoError(t, err)

			err = client.PutObject(t.Context(), "media/1/a.webp", "image/webp", []byte("payload"))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			req := <-got
			assert.Equal(t, http.MethodPut, req.method)
			assert.Equal(t, "/blog-media/media/1/a.webp", req.path)
			assert.Equal(t, "image/webp", req.contentType)
			assert.Contains(t, string(req.body), "payload")
		})
	}
}

func TestPresignPutWithoutContentType(t *testing.T) {
	t.Parallel()

	presigner := &fakePresignClient{request: &v4.PresignedHTTPRequest{URL: "https://example.test/upload"}}
	client := &Client{bucket: "blog-media", presign: presigner}

	url, headers, err := client.PresignPut(t.Context(), "k", "  ", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "https://example.test/upload", url)
	assert.NotNil(t, headers)
	assert.Empty(t, headers)
}

func TestPresignPutSurfacesErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	client := &Client{bucket: "blog-media", presign: &fakePresignClient{err: boom}}

	url, headers, err := client.PresignPut(t.Context(), "k", "image/png", time.Minute)
	require.ErrorIs(t, err, boom)
	assert.Empty(t, url)
	assert.Nil(t, headers)
}

func TestPresignGetSurfacesErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	client := &Client{bucket: "blog-media", presignGet: &fakePresignClient{err: boom}}

	url, err := client.PresignGet(t.Context(), "k", time.Minute)
	require.ErrorIs(t, err, boom)
	assert.Empty(t, url)
}

func TestHeadObjectSurfacesStorageErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	client := &Client{bucket: "blog-media", head: &fakeHeadClient{err: boom}}

	_, err := client.HeadObject(t.Context(), "k")
	require.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, ports.ErrObjectNotFound)
}

func TestSignedHeadersSkipsHostAndEmptyValues(t *testing.T) {
	t.Parallel()

	got := signedHeaders(http.Header{
		"host":       {"example.test"},
		"X-Empty":    {},
		"X-Amz-Meta": {"a", "b"},
	})

	assert.Equal(t, map[string]string{"X-Amz-Meta": "a, b"}, got)
}

func TestIsObjectNotFound(t *testing.T) {
	t.Parallel()

	statusErr := func(code int) error {
		return &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: code}},
			Err:      errors.New("upstream"),
		}
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "NotFound code", err: &smithy.GenericAPIError{Code: "NotFound"}, want: true},
		{name: "NoSuchKey code", err: &smithy.GenericAPIError{Code: "NoSuchKey"}, want: true},
		{name: "404 code", err: &smithy.GenericAPIError{Code: "404"}, want: true},
		{name: "other api code", err: &smithy.GenericAPIError{Code: "AccessDenied"}, want: false},
		{name: "http 404 status", err: fmt.Errorf("wrapped: %w", statusErr(http.StatusNotFound)), want: true},
		{name: "http 500 status", err: statusErr(http.StatusInternalServerError), want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isObjectNotFound(tt.err))
		})
	}
}
