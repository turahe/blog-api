// Package imgproxy builds signed imgproxy URLs for media transforms. imgproxy reads the
// original from the bucket (s3://) and does all decoding, so image bombs and CPU cost stay
// out of the API process.
package imgproxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/core/media/ports"
)

// Config locates imgproxy and the bucket it reads originals from.
type Config struct {
	// BaseURL is the public imgproxy origin, usually behind a CDN.
	BaseURL string
	// Key and Salt are the hex-encoded IMGPROXY_KEY and IMGPROXY_SALT.
	Key, Salt string
	Bucket    string
	// TTL is the minimum lifetime of a URL. URLs expire between TTL and 2×TTL after they are
	// issued and stay identical within a window, so caches keep hitting.
	TTL time.Duration
}

// Signer implements ports.Transformer.
type Signer struct {
	base   string
	key    []byte
	salt   []byte
	bucket string
	ttl    time.Duration
	now    func() time.Time
}

var _ ports.Transformer = (*Signer)(nil)

// New validates cfg and returns a Signer.
func New(cfg Config) (*Signer, error) {
	base, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, errors.New("IMGPROXY_URL must be an absolute http(s) URL")
	}

	key, err := hex.DecodeString(strings.TrimSpace(cfg.Key))
	if err != nil || len(key) == 0 {
		return nil, errors.New("IMGPROXY_KEY must be non-empty hex")
	}

	salt, err := hex.DecodeString(strings.TrimSpace(cfg.Salt))
	if err != nil || len(salt) == 0 {
		return nil, errors.New("IMGPROXY_SALT must be non-empty hex")
	}

	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("imgproxy needs S3_BUCKET")
	}

	if cfg.TTL <= 0 {
		return nil, errors.New("media transform URL TTL must be positive")
	}

	return &Signer{
		base:   strings.TrimRight(base.String(), "/"),
		key:    key,
		salt:   salt,
		bucket: cfg.Bucket,
		ttl:    cfg.TTL,
		now:    time.Now,
	}, nil
}

// URL returns the signed imgproxy URL for asset resized per t.
func (s *Signer) URL(asset mediadomain.MediaAsset, t mediadomain.Transform) (string, error) {
	if t.Width < 1 {
		return "", fmt.Errorf("invalid width %d", t.Width)
	}

	source := base64.RawURLEncoding.EncodeToString([]byte("s3://" + s.bucket + "/" + asset.StorageKey))

	path := "/exp:" + strconv.FormatInt(s.expiry(), 10) +
		"/rs:fit:" + strconv.Itoa(t.Width) + ":0" +
		"/" + source + extension(t.Format)

	return s.base + "/" + s.sign(path) + path, nil
}

// expiry rounds up to the end of the next TTL window.
func (s *Signer) expiry() int64 {
	window := max(int64(s.ttl/time.Second), 1)

	return (s.now().Unix()/window + 2) * window
}

func (s *Signer) sign(path string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(s.salt)
	mac.Write([]byte(path))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func extension(format string) string {
	switch format {
	case "":
		return ""
	case mediadomain.FormatJPEG:
		return ".jpg"
	default:
		return "." + format
	}
}
