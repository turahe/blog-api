package secretbox

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func testBox(t *testing.T, fill byte) *Box {
	t.Helper()

	box, err := New(bytes.Repeat([]byte{fill}, KeySize))
	require.NoError(t, err)

	return box
}

func TestEncryptRoundTripAndTamper(t *testing.T) {
	t.Parallel()

	box := testBox(t, 1)

	a, err := box.Encrypt([]byte("secret"))
	require.NoError(t, err)

	b, err := box.Encrypt([]byte("secret"))
	require.NoError(t, err)
	require.NotEqual(t, a, b, "nonces differ")

	plain, err := box.Decrypt(a)
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), plain)

	_, err = testBox(t, 2).Decrypt(a)
	require.ErrorIs(t, err, ErrCiphertext, "a different key cannot open it")

	tampered := a[:len(a)-2] + "AA"
	_, err = box.Decrypt(tampered)
	require.ErrorIs(t, err, ErrCiphertext)

	_, err = box.Decrypt("plain")
	require.ErrorIs(t, err, ErrCiphertext)
}

func TestMACIsKeyed(t *testing.T) {
	t.Parallel()

	first, again := testBox(t, 1).MAC("code"), testBox(t, 1).MAC("code")
	require.Equal(t, first, again)
	require.NotEqual(t, testBox(t, 1).MAC("code"), testBox(t, 2).MAC("code"))
}

func TestParseKey(t *testing.T) {
	t.Parallel()

	raw := bytes.Repeat([]byte{7}, KeySize)

	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawURLEncoding} {
		key, err := ParseKey(enc.EncodeToString(raw))
		require.NoError(t, err)
		require.Equal(t, raw, key)
	}

	_, err := ParseKey(base64.StdEncoding.EncodeToString([]byte("short")))
	require.Error(t, err)

	_, err = ParseKey("%%%")
	require.Error(t, err)

	_, err = New([]byte("short"))
	require.Error(t, err)
}
