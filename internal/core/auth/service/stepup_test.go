package service_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyStepUpWithoutTwoFactorNeedsOnlyPassword(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)

	ok, err := f.svc.VerifyStepUp(t.Context(), f.userID, tfPassword, "")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = f.svc.VerifyStepUp(t.Context(), f.userID, "wrong", "")
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = f.svc.VerifyStepUp(t.Context(), uuid.New(), tfPassword, "")
	require.NoError(t, err)
	assert.False(t, ok, "an unknown account never passes")
}

func TestVerifyStepUpWithTwoFactorNeedsPasswordAndCode(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	backup := f.enroll(t)

	for name, tc := range map[string]struct{ password, code string }{
		"password only":  {tfPassword, ""},
		"wrong code":     {tfPassword, "000000"},
		"wrong password": {"wrong", f.code(t)},
	} {
		ok, err := f.svc.VerifyStepUp(t.Context(), f.userID, tc.password, tc.code)
		require.NoError(t, err, name)
		assert.False(t, ok, name)
	}

	ok, err := f.svc.VerifyStepUp(t.Context(), f.userID, tfPassword, f.code(t))
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = f.svc.VerifyStepUp(t.Context(), f.userID, tfPassword, backup[0])
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = f.svc.VerifyStepUp(t.Context(), f.userID, tfPassword, backup[0])
	require.NoError(t, err)
	assert.False(t, ok, "a backup code is consumed")
}
