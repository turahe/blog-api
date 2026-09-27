package persistence

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestIDByUUID(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	user := insertUser(t, tx)

	var want int64
	require.NoError(t, tx.Raw("SELECT id FROM users WHERE uuid = ?", user).Row().Scan(&want))

	tests := []struct {
		name    string
		ctx     context.Context
		id      uuid.UUID
		want    int64
		wantErr error
	}{
		{name: "known", ctx: t.Context(), id: user, want: want},
		{name: "unknown", ctx: t.Context(), id: uuid.New(), wantErr: errUnknownReference},
		{name: "database error", ctx: canceledContext(t), id: user, wantErr: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := idByUUID(tx.WithContext(tt.ctx), "users", tt.id)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOptionalIDByUUID(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	user := insertUser(t, tx)
	unknown := uuid.New()

	got, err := optionalIDByUUID(tx, "users", nil)
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = optionalIDByUUID(tx, "users", &user)
	require.NoError(t, err)
	require.NotNil(t, got)

	_, err = optionalIDByUUID(tx, "users", &unknown)
	require.ErrorIs(t, err, errUnknownReference)
}

func TestInvalidReference(t *testing.T) {
	t.Parallel()

	errInvalid := errors.New("invalid")
	errOther := errors.New("connection reset")

	tests := []struct {
		name    string
		err     error
		wantErr error
		wantMsg string
	}{
		{name: "nil", err: nil},
		{name: "unknown reference", err: errUnknownReference, wantErr: errInvalid, wantMsg: "invalid: parent_id not found"},
		{name: "other error passes through", err: errOther, wantErr: errOther, wantMsg: "connection reset"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := invalidReference(tt.err, errInvalid, "parent_id")
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, tt.wantErr)
			require.EqualError(t, err, tt.wantMsg)
		})
	}
}

func TestReferenceSQLFragments(t *testing.T) {
	t.Parallel()

	ref := uuidRef("users", "posts.author_id", "author_uuid")
	require.Equal(t, "(SELECT ref.uuid FROM users ref WHERE ref.id = posts.author_id) AS author_uuid", ref)
	require.Equal(t, "posts.*", withRefs("posts"))
	require.Equal(t, "posts.*, "+ref, withRefs("posts", ref))
	require.Equal(t, "(SELECT id FROM tags WHERE uuid = ?)", idOf("tags"))
}
