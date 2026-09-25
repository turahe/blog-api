package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/settings/domain"
)

func TestDecodeByType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		def    domain.Definition
		raw    string
		want   any
		reason string
	}{
		{name: "string", def: domain.Definition{Type: domain.TypeString, MaxLength: 5}, raw: `"hello"`, want: "hello"},
		{name: "string too long", def: domain.Definition{Type: domain.TypeString, MaxLength: 5}, raw: `"hello!"`, reason: domain.ReasonTooLong},
		{name: "string control chars", def: domain.Definition{Type: domain.TypeString}, raw: `"a\nb"`, reason: domain.ReasonFormat},
		{name: "string enum", def: domain.Definition{Type: domain.TypeString, Enum: []string{"a", "b"}}, raw: `"c"`, reason: domain.ReasonNotAllowed},
		{name: "number is not a string", def: domain.Definition{Type: domain.TypeString}, raw: `1`, reason: domain.ReasonTypeMismatch},
		{name: "null", def: domain.Definition{Type: domain.TypeString}, raw: `null`, reason: domain.ReasonTypeMismatch},
		{name: "integer", def: domain.Definition{Type: domain.TypeInteger, Min: 1, Max: 10}, raw: `7`, want: int64(7)},
		{name: "integer range", def: domain.Definition{Type: domain.TypeInteger, Min: 1, Max: 10}, raw: `11`, reason: domain.ReasonOutOfRange},
		{name: "fraction is not integer", def: domain.Definition{Type: domain.TypeInteger, Max: 10}, raw: `1.5`, reason: domain.ReasonTypeMismatch},
		{name: "quoted integer", def: domain.Definition{Type: domain.TypeInteger, Max: 10}, raw: `"5"`, reason: domain.ReasonTypeMismatch},
		{name: "boolean", def: domain.Definition{Type: domain.TypeBoolean}, raw: `false`, want: false},
		{name: "quoted boolean", def: domain.Definition{Type: domain.TypeBoolean}, raw: `"true"`, reason: domain.ReasonTypeMismatch},
		{name: "numeric boolean", def: domain.Definition{Type: domain.TypeBoolean}, raw: `1`, reason: domain.ReasonTypeMismatch},
		{name: "list", def: domain.Definition{Type: domain.TypeStringList, Enum: []string{"a", "b"}}, raw: `["b","a"]`, want: []string{"b", "a"}},
		{name: "empty list", def: domain.Definition{Type: domain.TypeStringList}, raw: `[]`, want: []string{}},
		{name: "list item enum", def: domain.Definition{Type: domain.TypeStringList, Enum: []string{"a"}}, raw: `["x"]`, reason: domain.ReasonNotAllowed},
		{name: "list repeat", def: domain.Definition{Type: domain.TypeStringList}, raw: `["a","a"]`, reason: domain.ReasonNotAllowed},
		{name: "list too long", def: domain.Definition{Type: domain.TypeStringList, MaxItems: 1}, raw: `["a","b"]`, reason: domain.ReasonTooMany},
		{name: "list of numbers", def: domain.Definition{Type: domain.TypeStringList}, raw: `[1]`, reason: domain.ReasonTypeMismatch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.def.Key = "test.key"

			got, v := tc.def.Decode(json.RawMessage(tc.raw))
			if tc.reason != "" {
				require.NotNil(t, v)
				assert.Equal(t, tc.reason, v.Reason)
				assert.Equal(t, "test.key", v.Key)

				return
			}

			require.Nil(t, v)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDefaultCatalogueChecks(t *testing.T) {
	t.Parallel()

	catalogue := domain.DefaultCatalogue()

	cases := []struct {
		key   string
		raw   string
		valid bool
	}{
		{"site.timezone", `"Europe/Berlin"`, true},
		{"site.timezone", `"Mars/Olympus"`, false},
		{"site.timezone", `"../../etc/passwd"`, false},
		{"site.public_url", `"https://blog.example.com"`, true},
		{"site.public_url", `""`, true},
		{"site.public_url", `"javascript:alert(1)"`, false},
		{"site.public_url", `"https://user:pw@example.com"`, false},
		{"site.default_locale", `"pt-BR"`, true},
		{"site.default_locale", `"english"`, false},
		{"site.name", `"<script>"`, false},
		{"seo.title_template", `"{title} - {site}"`, true},
		{"seo.title_template", `"{site}"`, false},
		{"notifications.moderation_recipient_roles", `["editor"]`, true},
		{"notifications.moderation_recipient_roles", `["author"]`, false},
	}

	for _, tc := range cases {
		def, ok := catalogue.Lookup(tc.key)
		require.True(t, ok, tc.key)

		_, v := def.Decode(json.RawMessage(tc.raw))
		assert.Equal(t, tc.valid, v == nil, "%s = %s: %+v", tc.key, tc.raw, v)
	}
}

func TestDefaultCatalogueIsWellFormed(t *testing.T) {
	t.Parallel()

	defs := domain.DefaultCatalogue().Definitions()
	require.NotEmpty(t, defs)

	for i, def := range defs {
		assert.True(t, domain.ValidCategory(def.Category), def.Key)
		assert.NotEmpty(t, def.Description, def.Key)
		assert.NotEqual(t, domain.ServerOnly, def.Sensitivity, "%s: no server_only keys ship yet", def.Key)

		if i > 0 {
			assert.Less(t, defs[i-1].Key, def.Key, "sorted and unique")
		}
	}
}

func TestNewCataloguePanicsOnProgrammingErrors(t *testing.T) {
	t.Parallel()

	def := domain.Definition{Key: "a.b", Category: domain.CategorySite, Type: domain.TypeInteger, Default: int64(5), Max: 10}

	assert.Panics(t, func() { domain.NewCatalogue(def, def) }, "duplicate key")

	bad := def
	bad.Default = int64(50)

	assert.Panics(t, func() { domain.NewCatalogue(bad) }, "default out of range")
}

func TestValuesAccessors(t *testing.T) {
	t.Parallel()

	values := domain.NewValues(map[string]any{"s": "x", "i": int64(3), "b": true, "l": []string{"a"}})

	assert.Equal(t, "x", values.String("s"))
	assert.Equal(t, int64(3), values.Int("i"))
	assert.True(t, values.Bool("b"))
	assert.Equal(t, []string{"a"}, values.Strings("l"))
	assert.Empty(t, values.String("i"), "wrong type reads as zero")

	list := values.Strings("l")
	list[0] = "mutated"

	assert.Equal(t, []string{"a"}, values.Strings("l"), "Strings returns a copy")
}
