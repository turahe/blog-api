package messaging

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ThreeDotsLabs/watermill-googlecloud/v2/pkg/googlecloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

//nolint:paralleltest // sets PUBSUB_EMULATOR_HOST
func TestOpenGooglePubSub(t *testing.T) {
	// The emulator address is never dialled: the Pub/Sub client connects lazily.
	t.Setenv("PUBSUB_EMULATOR_HOST", closedAddr(t))

	tests := []struct {
		name     string
		instance string
	}{
		{name: "shared subscription"},
		{name: "broadcast subscription", instance: "api-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{MessageBroker: "pubsub", GooglePubSubProjectID: "blog-test"}

			var (
				bus *Bus
				err error
			)

			if tt.instance == "" {
				bus, err = Open(context.Background(), cfg, nil)
			} else {
				bus, err = OpenBroadcast(context.Background(), cfg, nil, tt.instance)
			}

			require.NoError(t, err)

			assert.Equal(t, BrokerGooglePubSub, bus.Broker)
			assert.IsType(t, &googlecloud.Publisher{}, bus.Publisher)
			assert.IsType(t, &googlecloud.Subscriber{}, bus.Subscriber)
			require.NoError(t, bus.Close())
		})
	}
}

//nolint:paralleltest // clears PUBSUB_EMULATOR_HOST so the credentials file is used
func TestOpenGooglePubSubErrors(t *testing.T) {
	t.Setenv("PUBSUB_EMULATOR_HOST", "")

	garbage := filepath.Join(t.TempDir(), "creds.json")
	require.NoError(t, os.WriteFile(garbage, []byte("not json"), 0o600))

	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{name: "unsupported credentials source", source: "vault:x", wantErr: `unsupported GOOGLE_PUBSUB_CREDENTIALS_SOURCE "vault:x"`},
		{name: "unreadable credentials", source: "path:" + garbage, wantErr: "open google pubsub publisher"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus, err := Open(context.Background(), config.Config{
				MessageBroker: "googlepubsub", GooglePubSubProjectID: "blog-test", GooglePubSubCredentialsSource: tt.source,
			}, nil)

			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, bus)
		})
	}
}

func TestGoogleCredentialsOptions(t *testing.T) {
	t.Parallel()

	creds := filepath.Join(t.TempDir(), "creds.json")
	require.NoError(t, os.WriteFile(creds, []byte("{}"), 0o600))

	tests := []struct {
		name     string
		source   string
		wantOpts int
		wantErr  string
	}{
		{name: "empty source uses adc", source: ""},
		{name: "workload identity", source: " workload-identity "},
		{name: "adc", source: "adc"},
		{name: "credentials file", source: "path: " + creds, wantOpts: 1},
		{name: "empty path", source: "path: ", wantErr: "GOOGLE_PUBSUB_CREDENTIALS_SOURCE path is empty"},
		{name: "missing credentials file", source: "path:" + filepath.Join(t.TempDir(), "missing.json"), wantErr: "credentials file"},
		{name: "unsupported source", source: "env", wantErr: `unsupported GOOGLE_PUBSUB_CREDENTIALS_SOURCE "env"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := googleCredentialsOptions(tt.source)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Nil(t, opts)

				return
			}

			require.NoError(t, err)
			assert.Len(t, opts, tt.wantOpts)
		})
	}
}
