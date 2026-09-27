package messaging

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/xdg-go/scram"
)

// writeCA writes a self-signed CA certificate as PEM and returns its path.
func writeCA(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "blog test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))

	return path
}

func TestApplyKafkaSecurityPlaintextByDefault(t *testing.T) {
	t.Parallel()

	sc := sarama.NewConfig()
	require.NoError(t, applyKafkaSecurity(sc, config.Config{}))
	require.False(t, sc.Net.TLS.Enable)
	require.False(t, sc.Net.SASL.Enable)
}

func TestApplyKafkaSecuritySCRAMOverTLS(t *testing.T) {
	t.Parallel()

	sc := sarama.NewConfig()
	require.NoError(t, applyKafkaSecurity(sc, config.Config{
		KafkaTLS:           true,
		KafkaSASLMechanism: config.KafkaSASLSCRAMSHA256,
		KafkaSASLUsername:  "blog",
		KafkaSASLPassword:  "secret",
	}))

	require.True(t, sc.Net.TLS.Enable)
	require.True(t, sc.Net.SASL.Enable)
	require.Equal(t, sarama.SASLMechanism(sarama.SASLTypeSCRAMSHA256), sc.Net.SASL.Mechanism)
	require.NoError(t, sc.Validate())

	client := sc.Net.SASL.SCRAMClientGeneratorFunc()
	require.NoError(t, client.Begin("blog", "secret", ""))

	first, err := client.Step("")
	require.NoError(t, err)
	require.Contains(t, first, "n=blog")
	require.False(t, client.Done())
}

func TestApplyKafkaSecurityMechanisms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		mechanism     string
		want          sarama.SASLMechanism
		wantGenerator bool
		wantErr       string
	}{
		{name: "plain", mechanism: config.KafkaSASLPlain, want: sarama.SASLTypePlaintext},
		{name: "scram sha256", mechanism: config.KafkaSASLSCRAMSHA256, want: sarama.SASLTypeSCRAMSHA256, wantGenerator: true},
		{name: "scram sha512", mechanism: config.KafkaSASLSCRAMSHA512, want: sarama.SASLTypeSCRAMSHA512, wantGenerator: true},
		{name: "unsupported", mechanism: "GSSAPI", wantErr: `unsupported KAFKA_SASL_MECHANISM "GSSAPI"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sc := sarama.NewConfig()
			err := applyKafkaSecurity(sc, config.Config{
				KafkaSASLMechanism: tt.mechanism, KafkaSASLUsername: "blog", KafkaSASLPassword: "secret",
			})

			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.True(t, sc.Net.SASL.Enable)
			assert.True(t, sc.Net.SASL.Handshake)
			assert.Equal(t, "blog", sc.Net.SASL.User)
			assert.Equal(t, "secret", sc.Net.SASL.Password)
			assert.Equal(t, tt.want, sc.Net.SASL.Mechanism)
			assert.False(t, sc.Net.TLS.Enable)

			if !tt.wantGenerator {
				assert.Nil(t, sc.Net.SASL.SCRAMClientGeneratorFunc)
				return
			}

			require.NotNil(t, sc.Net.SASL.SCRAMClientGeneratorFunc)
			client := sc.Net.SASL.SCRAMClientGeneratorFunc()
			require.NoError(t, client.Begin("blog", "secret", ""))

			first, err := client.Step("")
			require.NoError(t, err)
			assert.Contains(t, first, "n=blog")
		})
	}
}

func TestApplyKafkaSecurityRejectsBadCA(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, []byte("not a certificate"), 0o600))

	err := applyKafkaSecurity(sarama.NewConfig(), config.Config{KafkaTLS: true, KafkaTLSCAPath: path})
	require.ErrorContains(t, err, "no PEM certificates")
}

func TestKafkaTLSConfig(t *testing.T) {
	t.Parallel()

	ca := writeCA(t)

	tests := []struct {
		name      string
		path      string
		wantRoots bool
		wantErr   string
	}{
		{name: "system roots without CA path"},
		{name: "CA bundle pins roots", path: ca, wantRoots: true},
		{name: "missing CA file", path: filepath.Join(t.TempDir(), "missing.pem"), wantErr: "read KAFKA_TLS_CA_PATH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := kafkaTLSConfig(tt.path)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.ErrorIs(t, err, os.ErrNotExist)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, uint16(tls.VersionTLS12), got.MinVersion)
			assert.Equal(t, tt.wantRoots, got.RootCAs != nil)
		})
	}
}

func TestScramClientBegin(t *testing.T) {
	t.Parallel()

	client := &scramClient{hash: scram.SHA256}

	err := client.Begin("bad\u0000user", "secret", "")
	require.ErrorContains(t, err, "scram client")
	assert.Nil(t, client.conversation)
}

func TestScramClientStep(t *testing.T) {
	t.Parallel()

	client := &scramClient{hash: scram.SHA512}
	require.NoError(t, client.Begin("blog", "secret", ""))

	_, err := client.Step("")
	require.NoError(t, err)

	_, err = client.Step("not a server-first message")
	require.ErrorContains(t, err, "scram step")
	assert.False(t, client.Done())
}
