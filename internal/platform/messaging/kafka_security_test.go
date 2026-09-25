package messaging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

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

func TestApplyKafkaSecurityRejectsBadCA(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, []byte("not a certificate"), 0o600))

	err := applyKafkaSecurity(sarama.NewConfig(), config.Config{KafkaTLS: true, KafkaTLSCAPath: path})
	require.ErrorContains(t, err, "no PEM certificates")
}
