package config

import (
	"testing"
	"time"
)

func TestParseMIMEList(t *testing.T) {
	t.Parallel()
	got := ParseMIMEList("image/png, image/jpeg ,image/webp")
	if len(got) != 3 || got[0] != "image/png" || got[2] != "image/webp" {
		t.Fatalf("got %#v", got)
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
