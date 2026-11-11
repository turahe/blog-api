package pagination_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

const (
	keyA = "test-key-a-do-not-use-in-production-abc"
	keyB = "test-key-b-do-not-use-in-production-xyz"
)

func TestSignerVerifySameKey(t *testing.T) {
	t.Parallel()
	s := pagination.NewSigner([]byte(keyA), nil)
	payload := []byte(`hello`)
	signed := s.Sign(payload)
	out, err := s.Verify(signed)
	require.NoError(t, err)
	require.Equal(t, payload, out)
}

func TestSignerVerifyWrongKeyFails(t *testing.T) {
	t.Parallel()
	s1 := pagination.NewSigner([]byte(keyA), nil)
	s2 := pagination.NewSigner([]byte(keyB), nil)
	signed := s1.Sign([]byte(`hello`))
	_, err := s2.Verify(signed)
	require.ErrorIs(t, err, pagination.ErrCursorInvalidSignature)
	require.Equal(t, pagination.CodeTampered, pagination.ErrorCode(err))
}

func TestSignerVerifyEmptyEnvReturnsError(t *testing.T) {
	pagination.ResetGlobalSigner()
	t.Setenv("PAGINATION_HMAC_KEY", "")
	t.Setenv("APP_KEY", "")
	_, err := pagination.NewSignerFromEnv()
	require.Error(t, err)
}

func TestSignerFallbackAppKey(t *testing.T) {
	pagination.ResetGlobalSigner()
	t.Setenv("PAGINATION_HMAC_KEY", "")
	t.Setenv("APP_KEY", keyA)
	s, err := pagination.NewSignerFromEnv()
	require.NoError(t, err)
	signed := s.Sign([]byte(`x`))
	_, err = s.Verify(signed)
	require.NoError(t, err)
}

func TestSignerPrefersPaginationHMACKey(t *testing.T) {
	t.Setenv("PAGINATION_HMAC_KEY", keyA)
	t.Setenv("APP_KEY", keyB)
	s, err := pagination.NewSignerFromEnv()
	require.NoError(t, err)
	signed := s.Sign([]byte(`x`))
	appKey := pagination.NewSigner([]byte(keyB), nil)
	_, err = appKey.Verify(signed)
	require.ErrorIs(t, err, pagination.ErrCursorInvalidSignature)
}

func TestSignerKeyRotationPreviousVerifies(t *testing.T) {
	t.Parallel()
	old := pagination.NewSigner([]byte(keyA), nil)
	token := old.Sign([]byte(`data`))
	new := pagination.NewSigner([]byte(keyB), []byte(keyA))
	out, err := new.Verify(token)
	require.NoError(t, err)
	require.Equal(t, []byte(`data`), out)
	// NEW tokens signed by the "new" signer cannot be verified by old.
	newToken := new.Sign([]byte(`data2`))
	_, err = old.Verify(newToken)
	require.ErrorIs(t, err, pagination.ErrCursorInvalidSignature)
}

func TestGlobalSignerCached(t *testing.T) {
	pagination.ResetGlobalSigner()
	t.Setenv("PAGINATION_HMAC_KEY", keyA)
	t.Setenv("APP_KEY", "fallback")
	a, err := pagination.GlobalSigner()
	require.NoError(t, err)
	t.Setenv("APP_KEY", "")
	t.Setenv("PAGINATION_HMAC_KEY", "")
	b, err := pagination.GlobalSigner()
	require.NoError(t, err)
	require.Same(t, a, b)
}

func TestSignerMalformedSignatures(t *testing.T) {
	t.Parallel()
	s := pagination.NewSigner([]byte(keyA), nil)
	cases := []struct {
		name string
		in   []byte
	}{
		{"empty", []byte(``)},
		{"no separator", []byte(`nodash`)},
		{"separator but no mac", []byte(`foo|`)},
		{"separator but no payload", []byte(`|ff`)},
		{"invalid hex mac", []byte(`foo|gggg`)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := s.Verify(tc.in)
			require.ErrorIs(t, err, pagination.ErrCursorInvalidSignature)
		})
	}
}
