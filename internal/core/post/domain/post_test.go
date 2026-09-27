package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommentPolicyValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy CommentPolicy
		want   bool
	}{
		{name: "open", policy: CommentPolicyOpen, want: true},
		{name: "authenticated", policy: CommentPolicyAuthenticated, want: true},
		{name: "read only", policy: CommentPolicyReadOnly, want: true},
		{name: "disabled", policy: CommentPolicyDisabled, want: true},
		{name: "empty", policy: "", want: false},
		{name: "unknown", policy: "moderated", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.policy.Valid())
		})
	}
}

func TestTransitionNext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		transition Transition
		from       Status
		want       Status
		wantErr    error
	}{
		{name: "publish a draft", transition: TransitionPublish, from: StatusDraft, want: StatusPublished},
		{name: "publish a scheduled post", transition: TransitionPublish, from: StatusScheduled, want: StatusPublished},
		{name: "republish an archived post", transition: TransitionPublish, from: StatusArchived, want: StatusPublished},
		{name: "unpublish", transition: TransitionUnpublish, from: StatusPublished, want: StatusDraft},
		{name: "archive", transition: TransitionArchive, from: StatusPublished, want: StatusArchived},
		{name: "publish twice", transition: TransitionPublish, from: StatusPublished, wantErr: ErrInvalidTransition},
		{name: "unpublish a draft", transition: TransitionUnpublish, from: StatusDraft, wantErr: ErrInvalidTransition},
		{name: "archive twice", transition: TransitionArchive, from: StatusArchived, wantErr: ErrInvalidTransition},
		{name: "unknown transition", transition: "delete", from: StatusDraft, wantErr: ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.transition.Next(tt.from)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInvalidTransitionIsAConflict(t *testing.T) {
	t.Parallel()

	_, err := TransitionArchive.Next(StatusArchived)
	require.ErrorIs(t, err, ErrConflict)
}
