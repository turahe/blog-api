package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		transition Transition
		want       bool
	}{
		{name: "internal", transition: TransitionInternal, want: true},
		{name: "external", transition: TransitionExternal, want: true},
		{name: "back forward", transition: TransitionBackForward, want: true},
		{name: "direct", transition: TransitionDirect, want: true},
		{name: "empty", transition: "", want: false},
		{name: "unknown", transition: "warp", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, ValidTransition(tt.transition))
		})
	}
}

func TestValidResourceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resource ResourceType
		want     bool
	}{
		{name: "post", resource: ResourcePost, want: true},
		{name: "page", resource: ResourcePage, want: true},
		{name: "category", resource: ResourceCategory, want: true},
		{name: "tag", resource: ResourceTag, want: true},
		{name: "empty", resource: "", want: false},
		{name: "user", resource: "user", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, ValidResourceType(tt.resource))
		})
	}
}
