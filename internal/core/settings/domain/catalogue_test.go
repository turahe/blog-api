package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/settings/domain"
)

func TestNewCatalogue(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)

		cat := domain.NewCatalogue()
		defs := cat.Definitions()
		is.Empty(defs)

		_, ok := cat.Lookup("any.key")
		is.False(ok)
	})

	t.Run("sorts definitions by key regardless of input order", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)

		b := domain.Definition{Key: "site.b", Category: domain.CategorySite, Type: domain.TypeBoolean, Default: false}
		a := domain.Definition{Key: "site.a", Category: domain.CategorySite, Type: domain.TypeBoolean, Default: false}
		c := domain.Definition{Key: "site.c", Category: domain.CategorySite, Type: domain.TypeBoolean, Default: false}

		cat := domain.NewCatalogue(b, a, c)

		defs := cat.Definitions()
		require.Len(t, defs, 3)
		is.Equal("site.a", defs[0].Key)
		is.Equal("site.b", defs[1].Key)
		is.Equal("site.c", defs[2].Key)
	})

	t.Run("does not mutate the caller's slice", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)

		a := domain.Definition{Key: "site.a", Category: domain.CategorySite, Type: domain.TypeBoolean, Default: false}
		b := domain.Definition{Key: "site.b", Category: domain.CategorySite, Type: domain.TypeBoolean, Default: false}

		input := []domain.Definition{b, a}
		_ = domain.NewCatalogue(input...)

		is.Equal("site.b", input[0].Key, "caller slice order preserved")
		is.Equal("site.a", input[1].Key)
	})
}

func TestCatalogueLookup(t *testing.T) {
	t.Parallel()

	def := domain.Definition{
		Key:         "feature.flag",
		Category:    domain.CategorySite,
		Type:        domain.TypeBoolean,
		Sensitivity: domain.PublicSafe,
		Default:     true,
	}
	cat := domain.NewCatalogue(def)

	t.Run("hit", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)

		got, ok := cat.Lookup("feature.flag")
		is.True(ok)
		is.Equal("feature.flag", got.Key)
		is.Equal(domain.TypeBoolean, got.Type)
		is.Equal(true, got.Default)
	})

	t.Run("miss", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)

		got, ok := cat.Lookup("missing.key")
		is.False(ok)
		is.Empty(got.Key)
	})
}

func TestCatalogueDefinitionsIsDefensiveCopy(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	a := domain.Definition{Key: "site.a", Category: domain.CategorySite, Type: domain.TypeBoolean, Default: false}
	b := domain.Definition{Key: "site.b", Category: domain.CategorySite, Type: domain.TypeBoolean, Default: false}
	cat := domain.NewCatalogue(a, b)

	first := cat.Definitions()
	require.Len(t, first, 2)

	first[0] = domain.Definition{Key: "mutated"}

	second := cat.Definitions()
	is.Equal("site.a", second[0].Key, "mutating the returned slice must not affect the catalogue")
	is.Equal("site.b", second[1].Key)
}

func TestValuesConstructors(t *testing.T) {
	t.Parallel()

	t.Run("nil map behaves like empty", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)

		v := domain.NewValues(nil)
		is.Empty(v.String("anything"))
		is.Equal(int64(0), v.Int("anything"))
		is.False(v.Bool("anything"))
		is.Nil(v.Strings("anything"))
	})

	t.Run("empty map returns zero values", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)

		v := domain.NewValues(map[string]any{})
		is.Empty(v.String("missing"))
		is.Equal(int64(0), v.Int("missing"))
		is.False(v.Bool("missing"))
		is.Nil(v.Strings("missing"))
	})
}

func TestValuesAccessorsEdgeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		key   string
		value any
	}{
		{name: "string instead of int", key: "i", value: "not-a-number"},
		{name: "string instead of bool", key: "b", value: "nope"},
		{name: "int instead of string", key: "s", value: int64(42)},
		{name: "int instead of bool", key: "b", value: int64(1)},
		{name: "bool instead of string", key: "s", value: true},
		{name: "bool instead of int", key: "i", value: true},
		{name: "int instead of list", key: "l", value: int64(1)},
		{name: "string instead of list", key: "l", value: "oops"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			v := domain.NewValues(map[string]any{tc.key: tc.value})
			is.Empty(v.String("s"), "string with wrong type returns zero value")
			is.Equal(int64(0), v.Int("i"), "int with wrong type returns zero value")
			is.False(v.Bool("b"), "bool with wrong type returns zero value")
			is.Nil(v.Strings("l"), "strings with wrong type returns nil")
		})
	}
}

func TestValuesStringsClonesSlice(t *testing.T) {
	t.Parallel()

	t.Run("nil list returns nil", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)

		v := domain.NewValues(map[string]any{"list": ([]string)(nil)})
		is.Nil(v.Strings("list"))
	})

	t.Run("mutations to returned list do not reach stored value", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)

		original := []string{"a", "b"}
		v := domain.NewValues(map[string]any{"list": original})

		got := v.Strings("list")
		is.Equal([]string{"a", "b"}, got)

		got[0] = "changed"
		is.Equal([]string{"a", "b"}, v.Strings("list"), "stored slice untouched")
		is.Equal([]string{"a", "b"}, original, "caller's original slice untouched")
	})
}
