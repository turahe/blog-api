package responses

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIPPrefixCoarsensAddresses(t *testing.T) {
	t.Parallel()

	cases := map[string]*string{
		"203.0.113.77":        new("203.0.113.0/24"),
		"::ffff:198.51.100.9": new("198.51.100.0/24"),
		"2001:db8:abcd:12::1": new("2001:db8:abcd::/48"),
		"":                    nil,
		"not-an-ip":           nil,
	}

	for in, want := range cases {
		require.Equal(t, want, ipPrefix(in), in)
	}
}

func TestDeviceSummarizesUserAgent(t *testing.T) {
	t.Parallel()

	cases := map[string]*string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/128.0 Safari/537.36 Edg/128.0":     new("Edge on Windows"),
		"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/128.0 Mobile Safari/537.36":                  new("Chrome on Android"),
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Safari/604.1": new("Safari on iOS"),
		"curl/8.5.0":  new("curl"),
		"SomeBot/1.0": new("Unknown browser"),
		"":            nil,
	}

	for in, want := range cases {
		require.Equal(t, want, device(in), in)
	}
}
