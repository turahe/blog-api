package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"/":                          "/",
		"/posts/hello/":              "/posts/hello",
		"  /posts//hello  ":          "/posts/hello",
		"/posts/hello?token=secret":  "/posts/hello",
		"/posts/hello#section":       "/posts/hello",
		"/search?q=x#y":              "/search",
		"/caf%C3%A9":                 "/caf%C3%A9",
		"//evil.example/posts/hello": "/evil.example/posts/hello",
	} {
		got, err := NormalizePath(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}

	for _, raw := range []string{"", "posts", "https://example.com/", "/a b", "/a\x00b", "/" + strings.Repeat("a", MaxPathLength)} {
		_, err := NormalizePath(raw)
		require.ErrorIs(t, err, ErrValidation, raw)
	}
}

func TestNormalizeReferrer(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"https://Example.com/a/b?x=1#f": "https://example.com/a/b",
		"https://user:pw@example.com/":  "https://example.com",
		"http://example.com":            "http://example.com",
		"android-app://com.example":     "",
		"javascript:alert(1)":           "",
		"not a url":                     "",
		"":                              "",
	} {
		assert.Equal(t, want, NormalizeReferrer(raw), raw)
	}

	long := "https://example.com/" + strings.Repeat("p", MaxReferrerLength)
	assert.Equal(t, "https://example.com", NormalizeReferrer(long), "an overlong path is dropped")
}

func TestNormalizeQuery(t *testing.T) {
	t.Parallel()

	got, err := NormalizeQuery("  Hello\tWORLD \n go\x00lang ")
	require.NoError(t, err)
	assert.Equal(t, "hello world go lang", got)

	got, err = NormalizeQuery(strings.Repeat("é", MaxQueryLength+10))
	require.NoError(t, err)
	assert.Len(t, []rune(got), MaxQueryLength)

	_, err = NormalizeQuery(" \t ")
	require.ErrorIs(t, err, ErrValidation)
}

func TestNormalizeFilters(t *testing.T) {
	t.Parallel()

	got, err := NormalizeFilters(map[string]string{"tag": " go ", "category": ""})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"tag": "go"}, got)

	_, err = NormalizeFilters(map[string]string{"author": "x"})
	require.ErrorIs(t, err, ErrValidation)

	_, err = NormalizeFilters(map[string]string{"tag": strings.Repeat("x", MaxFilterValue+1)})
	require.ErrorIs(t, err, ErrValidation)
}

func TestNormalizeCountry(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{"de": "DE", "XX": "", "T1": "", "DEU": "", "": ""} {
		assert.Equal(t, want, NormalizeCountry(raw), raw)
	}

	assert.Equal(t, "FR", NormalizeCountry(" FR "), "surrounding whitespace is trimmed")
}

func TestClampFocus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, ClampFocus(-5))
	assert.Equal(t, 30, ClampFocus(30))
	assert.Equal(t, MaxFocusSeconds, ClampFocus(MaxFocusSeconds+1))
}

func TestClassifyAgent(t *testing.T) {
	t.Parallel()

	cases := map[string]Agent{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36": {
			Device: DeviceDesktop, Browser: "chrome",
		},
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/140.0 Safari/537.36 Edg/140.0": {
			Device: DeviceDesktop, Browser: "edge",
		},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1": {
			Device: DeviceMobile, Browser: "safari",
		},
		"Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Safari/604.1": {
			Device: DeviceTablet, Browser: "safari",
		},
		"Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 Chrome/140.0 Mobile Safari/537.36": {
			Device: DeviceMobile, Browser: "chrome",
		},
		"Mozilla/5.0 (Linux; Android 15; SM-X910) AppleWebKit/537.36 Chrome/140.0 Safari/537.36": {
			Device: DeviceTablet, Browser: "chrome",
		},
		"Mozilla/5.0 (X11; Linux x86_64; rv:141.0) Gecko/20100101 Firefox/141.0": {Device: DeviceDesktop, Browser: "firefox"},
		"SomethingNew/1.0": {Device: DeviceDesktop, Browser: "other"},
	}
	for ua, want := range cases {
		assert.Equal(t, want, ClassifyAgent(ua), ua)
	}

	for _, ua := range []string{
		"", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "curl/8.9.1",
		"python-requests/2.32", "Mozilla/5.0 (X11; Linux x86_64) HeadlessChrome/140.0", "facebookexternalhit/1.1",
	} {
		assert.True(t, ClassifyAgent(ua).Bot, ua)
	}
}
