package imgproxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

func newSigner(t *testing.T, now time.Time) *Signer {
	t.Helper()

	s, err := New(Config{BaseURL: "https://img.example.com/", Key: "6b6579", Salt: "73616c74", Bucket: "blog-media", TTL: time.Hour})
	require.NoError(t, err)

	s.now = func() time.Time { return now }

	return s
}

func TestURLIsSignedAndEncodesTheBucketObject(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_800_000_000, 0)
	s := newSigner(t, now)

	got, err := s.URL(mediadomain.MediaAsset{StorageKey: "media/2026/a b.png"}, mediadomain.Transform{Width: 256, Format: "jpeg"})
	require.NoError(t, err)

	source := base64.RawURLEncoding.EncodeToString([]byte("s3://blog-media/media/2026/a b.png"))
	path := "/exp:1800007200/rs:fit:256:0/" + source + ".jpg"

	mac := hmac.New(sha256.New, []byte("key"))
	mac.Write([]byte("salt" + path))

	require.Equal(t, "https://img.example.com/"+base64.RawURLEncoding.EncodeToString(mac.Sum(nil))+path, got)
}

func TestURLStaysStableWithinAWindowAndExpires(t *testing.T) {
	t.Parallel()

	start := time.Unix(1_800_000_000, 0)
	asset := mediadomain.MediaAsset{StorageKey: "k.webp"}
	transform := mediadomain.Transform{Width: 512}

	first, err := newSigner(t, start).URL(asset, transform)
	require.NoError(t, err)

	later, err := newSigner(t, start.Add(10*time.Minute)).URL(asset, transform)
	require.NoError(t, err)
	require.Equal(t, first, later, "cache key does not change inside the window")

	next, err := newSigner(t, start.Add(time.Hour)).URL(asset, transform)
	require.NoError(t, err)
	require.NotEqual(t, first, next)
	require.NotContains(t, first, ".webp", "an empty format keeps the source format")
}

func TestNewRejectsBadConfig(t *testing.T) {
	t.Parallel()

	valid := Config{BaseURL: "http://localhost:8081", Key: "aa", Salt: "bb", Bucket: "b", TTL: time.Hour}

	for name, mutate := range map[string]func(*Config){
		"relative url": func(c *Config) { c.BaseURL = "/img" },
		"non-hex key":  func(c *Config) { c.Key = "not-hex" },
		"empty salt":   func(c *Config) { c.Salt = "" },
		"no bucket":    func(c *Config) { c.Bucket = "" },
		"zero ttl":     func(c *Config) { c.TTL = 0 },
	} {
		cfg := valid
		mutate(&cfg)

		_, err := New(cfg)
		require.Error(t, err, name)
	}

	_, err := New(valid)
	require.NoError(t, err)
}
