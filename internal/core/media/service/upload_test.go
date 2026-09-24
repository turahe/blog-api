package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/core/media/ports"
)

// tinyWebP is a 1x1 lossless WebP.
const tinyWebP = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewGray(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.White)

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

func mustBase64(t *testing.T, s string) []byte {
	t.Helper()

	data, err := base64.StdEncoding.DecodeString(s)
	require.NoError(t, err)

	return data
}

func TestCompleteUploadVerifiesContent(t *testing.T) {
	t.Parallel()

	html := []byte("<!DOCTYPE html><script>alert(1)</script>")
	cases := map[string]struct {
		declared string
		body     []byte
		readErr  error
		want     error
	}{
		"matching png":         {declared: "image/png", body: encodePNG(t, 1, 1)},
		"html declared as png": {declared: "image/png", body: html, want: ErrValidation},
		"png declared as webp": {declared: "image/webp", body: encodePNG(t, 1, 1), want: ErrValidation},
		"storage read failure": {declared: "image/png", readErr: errors.New("boom"), want: ErrStorage},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			svc, repo, storage := newTestService()
			asset := repo.store(makePendingAsset())
			storage.headInfo[asset.StorageKey] = ports.ObjectInfo{Size: 64, ContentType: tc.declared}
			storage.objects[asset.StorageKey] = tc.body
			storage.readErr = tc.readErr

			got, err := svc.CompleteUpload(t.Context(), asset.UUID)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, mediadomain.StatusPending, repo.assets[asset.UUID].Status)

				return
			}

			require.NoError(t, err)
			require.Equal(t, mediadomain.StatusReady, got.Status)
		})
	}
}

func TestVerifyContent(t *testing.T) {
	t.Parallel()

	pdf := []byte("%PDF-1.7\n")
	html := []byte("<html><body>x</body></html>")
	xml := []byte("<?xml version=\"1.0\"?><svg/>")

	require.NoError(t, verifyContent("application/pdf", pdf))
	require.ErrorIs(t, verifyContent("application/pdf", html), ErrValidation)
	require.NoError(t, verifyContent("text/plain", []byte("plain notes")))
	require.ErrorIs(t, verifyContent("text/plain", html), ErrValidation)
	require.ErrorIs(t, verifyContent("video/mp4", xml), ErrValidation)
}

func TestUploadImageStoresSniffedImage(t *testing.T) {
	t.Parallel()

	svc, repo, storage := newTestService()
	owner := uuid.New()
	data := encodePNG(t, 3, 2)

	asset, err := svc.UploadImage(t.Context(), mediadomain.ImageUpload{
		UploadedBy: &owner, Filename: "evil.html", Data: data, Tags: []string{"avatar"},
	})
	require.NoError(t, err)
	require.Equal(t, "image/png", asset.ContentType, "the declared name never decides the type")
	require.Equal(t, "media/"+fixedID().String()+"/evil.png", asset.StorageKey)
	require.Equal(t, "image/png", storage.puts[asset.StorageKey])
	require.Equal(t, mediadomain.StatusReady, asset.Status)
	require.Equal(t, 3, *asset.Width)
	require.Equal(t, 2, *asset.Height)
	require.Len(t, *asset.ChecksumSHA256, 64)
	require.Equal(t, int64(len(data)), asset.SizeBytes)
	require.Equal(t, &owner, asset.UploadedByUUID)
	require.Contains(t, repo.assets, asset.UUID)
}

func TestUploadImageAcceptsWebP(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService()
	data, err := base64.StdEncoding.DecodeString(tinyWebP)
	require.NoError(t, err)

	asset, err := svc.UploadImage(t.Context(), mediadomain.ImageUpload{Filename: "a.webp", Data: data})
	require.NoError(t, err)
	require.Equal(t, "image/webp", asset.ContentType)
	require.Equal(t, 1, *asset.Width)
}

func TestUploadImageRejects(t *testing.T) {
	t.Parallel()

	var gifBuf bytes.Buffer
	require.NoError(t, gif.Encode(&gifBuf, image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black}), nil))

	pngBytes := encodePNG(t, 1, 1)
	truncated := append([]byte(nil), pngBytes[:20]...)

	cases := map[string]mediadomain.ImageUpload{
		"html disguised as png": {Filename: "a.png", Data: []byte("<html><script>alert(1)</script></html>")},
		"svg":                   {Filename: "a.svg", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		"type not allowed":      {Filename: "a.gif", Data: gifBuf.Bytes()},
		"empty":                 {Filename: "a.png", Data: nil},
		"over service limit":    {Filename: "a.png", Data: append(pngBytes, make([]byte, 4096)...)},
		"over caller limit":     {Filename: "a.png", Data: pngBytes, MaxBytes: 10},
		"corrupt header":        {Filename: "a.png", Data: truncated},
		"path traversal":        {Filename: "../../etc/passwd.png", Data: pngBytes},
		"control characters":    {Filename: "a\x00.png", Data: pngBytes},
		"overlong name":         {Filename: strings.Repeat("a", 300) + ".png", Data: pngBytes},
	}

	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			svc, repo, storage := newTestService()

			_, err := svc.UploadImage(t.Context(), input)
			require.ErrorIs(t, err, ErrValidation)
			require.Empty(t, storage.puts, "nothing is stored")
			require.Empty(t, repo.assets)
		})
	}
}

func TestUploadImageRejectsHugeDimensions(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService()
	svc.maxBytes = 1 << 20

	_, err := svc.UploadImage(t.Context(), mediadomain.ImageUpload{Filename: "wide.png", Data: encodePNG(t, MaxImageDimension+1, 1)})
	require.ErrorIs(t, err, ErrValidation)
	require.ErrorContains(t, err, "dimensions")
}

func TestUploadImageStorageFailure(t *testing.T) {
	t.Parallel()

	svc, repo, storage := newTestService()
	storage.putErr = errors.New("bucket down")

	_, err := svc.UploadImage(t.Context(), mediadomain.ImageUpload{Filename: "a.png", Data: encodePNG(t, 1, 1)})
	require.ErrorIs(t, err, ErrStorage)
	require.Empty(t, repo.assets)
}
