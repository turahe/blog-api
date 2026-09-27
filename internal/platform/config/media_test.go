package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseMIMEList(t *testing.T) {
	t.Parallel()

	got := ParseMIMEList("image/png, image/jpeg ,image/webp")
	if len(got) != 3 || got[0] != "image/png" || got[2] != "image/webp" {
		t.Fatalf("got %#v", got)
	}
}

func TestParseWidths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want []int
	}{
		{name: "valid", raw: "64, 128,8192", want: []int{64, 128, 8192}},
		{name: "empty", raw: " ", want: nil},
		{name: "not a number", raw: "64,wide", want: nil},
		{name: "zero", raw: "0,64", want: nil},
		{name: "too wide", raw: "64,8193", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, ParseWidths(tt.raw))
		})
	}
}

// validMedia returns media settings that pass ValidateMedia with transforms enabled.
func validMedia() Config {
	return Config{
		S3Bucket: "blog-media", S3AccessKey: "k", S3SecretKey: "s", S3Disk: "r2",
		MediaAllowedMIMETypes: []string{"image/png"}, MediaMaxUploadBytes: 10, MediaPresignTTL: time.Minute,
		AvatarMaxBytes: 10, PrivacyExportRetention: time.Hour, PrivacyExportURLTTL: time.Minute,
		AnalyticsExportRetention: time.Hour, AnalyticsExportURLTTL: time.Minute,
		ImgproxyURL: "http://imgproxy:8080", ImgproxyKey: "abcd", ImgproxySalt: "ef01",
		MediaTransformWidths: []int{256}, MediaTransformURLTTL: time.Hour,
	}
}

func TestValidateMediaRejects(t *testing.T) {
	t.Parallel()

	require.NoError(t, validMedia().ValidateMedia())

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "avatar size", mutate: func(c *Config) { c.AvatarMaxBytes = 0 }, wantErr: "AVATAR_MAX_BYTES"},
		{name: "negative purge", mutate: func(c *Config) { c.MediaPurgeAfter = -time.Second }, wantErr: "MEDIA_PURGE_AFTER"},
		{name: "export retention", mutate: func(c *Config) { c.PrivacyExportRetention = 0 }, wantErr: "PRIVACY_EXPORT_RETENTION"},
		{name: "export url ttl zero", mutate: func(c *Config) { c.PrivacyExportURLTTL = 0 }, wantErr: "PRIVACY_EXPORT_URL_TTL"},
		{name: "export url ttl too long", mutate: func(c *Config) { c.PrivacyExportURLTTL = maxPresignTTL + time.Second }, wantErr: "PRIVACY_EXPORT_URL_TTL"},
		{name: "missing imgproxy salt", mutate: func(c *Config) { c.ImgproxySalt = "" }, wantErr: "IMGPROXY_KEY and IMGPROXY_SALT are required"},
		{name: "no transform widths", mutate: func(c *Config) { c.MediaTransformWidths = nil }, wantErr: "MEDIA_TRANSFORM_WIDTHS"},
		{name: "transform url ttl", mutate: func(c *Config) { c.MediaTransformURLTTL = 0 }, wantErr: "MEDIA_TRANSFORM_URL_TTL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validMedia()
			tt.mutate(&cfg)

			require.ErrorContains(t, cfg.ValidateMedia(), tt.wantErr)
		})
	}
}

func TestValidateMediaDisabledOK(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	if err := cfg.ValidateMedia(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMediaRequiresDisk(t *testing.T) {
	t.Parallel()

	cfg := Config{
		S3Bucket:              "blog-media",
		S3AccessKey:           "k",
		S3SecretKey:           "s",
		S3Disk:                "ftp",
		MediaAllowedMIMETypes: []string{"image/png"},
		MediaMaxUploadBytes:   10,
		MediaPresignTTL:       time.Minute,
	}
	if err := cfg.ValidateMedia(); err == nil {
		t.Fatal("expected disk error")
	}
}

func TestValidateMediaRequiresAllowlist(t *testing.T) {
	t.Parallel()

	cfg := Config{
		S3Bucket:            "blog-media",
		S3AccessKey:         "k",
		S3SecretKey:         "s",
		S3Disk:              "minio",
		MediaMaxUploadBytes: 10,
		MediaPresignTTL:     time.Minute,
	}
	if err := cfg.ValidateMedia(); err == nil {
		t.Fatal("expected allowlist error")
	}
}

func TestValidateMediaRequiresPositiveMaxUploadBytes(t *testing.T) {
	t.Parallel()

	cfg := Config{
		S3Bucket:              "blog-media",
		S3AccessKey:           "k",
		S3SecretKey:           "s",
		S3Disk:                "minio",
		MediaAllowedMIMETypes: []string{"image/png"},
		MediaPresignTTL:       time.Minute,
	}
	if err := cfg.ValidateMedia(); err == nil {
		t.Fatal("expected max upload bytes error")
	}
}

func TestValidateMediaRequiresPositivePresignTTL(t *testing.T) {
	t.Parallel()

	cfg := Config{
		S3Bucket:              "blog-media",
		S3AccessKey:           "k",
		S3SecretKey:           "s",
		S3Disk:                "minio",
		MediaAllowedMIMETypes: []string{"image/png"},
		MediaMaxUploadBytes:   10,
	}
	if err := cfg.ValidateMedia(); err == nil {
		t.Fatal("expected presign ttl error")
	}
}

func TestLoadMediaDefaults(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("S3_BUCKET", "blog-media")
	t.Setenv("S3_ACCESS_KEY", "minioadmin")
	t.Setenv("S3_SECRET_KEY", "minioadmin")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.MediaEnabled() {
		t.Fatal("expected MediaEnabled")
	}

	if cfg.MediaMaxUploadBytes != 10<<20 {
		t.Fatalf("max=%d", cfg.MediaMaxUploadBytes)
	}

	if len(cfg.MediaAllowedMIMETypes) < 4 {
		t.Fatalf("allowlist=%v", cfg.MediaAllowedMIMETypes)
	}
}

func TestValidateTransformsKeyStrength(t *testing.T) {
	t.Parallel()

	base := Config{
		S3Bucket:             "blog-media",
		S3AccessKey:          "k",
		S3SecretKey:          "s",
		ImgproxyURL:          "http://imgproxy:8080",
		ImgproxyKey:          "abcd",
		ImgproxySalt:         "ef01",
		MediaTransformWidths: []int{256},
		MediaTransformURLTTL: time.Hour,
	}
	if err := base.validateTransforms(); err != nil {
		t.Fatalf("short keys are allowed outside production: %v", err)
	}

	notHex := base
	notHex.ImgproxyKey = "not-hex"

	if err := notHex.validateTransforms(); err == nil {
		t.Fatal("expected hex error")
	}

	production := base
	production.Environment = envProduction

	if err := production.validateTransforms(); err == nil {
		t.Fatal("expected short key error in production")
	}

	production.ImgproxyKey = strings.Repeat("ab", minImgproxyKeyBytes)
	production.ImgproxySalt = strings.Repeat("cd", minImgproxySaltBytes)

	if err := production.validateTransforms(); err != nil {
		t.Fatal(err)
	}
}
