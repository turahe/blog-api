package persistence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/notification/template"
)

func TestNotificationTemplateRepository(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := context.Background()
	repo := NewNotificationTemplateRepository(tx)

	require.NoError(t, tx.Exec("DELETE FROM notification_templates").Error)
	require.NoError(t, repo.SeedDefaults(ctx, template.Defaults()))

	rows, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, len(template.Defaults()))

	custom := template.Template{
		Type: template.TypePasswordChanged, Channel: template.ChannelEmail,
		Subject: "Custom subject", Body: "Hi {{.Name}}",
	}
	require.NoError(t, repo.Save(ctx, custom))

	got, err := repo.Get(ctx, template.TypePasswordChanged, template.ChannelEmail)
	require.NoError(t, err)
	require.Equal(t, "Custom subject", got.Subject)

	require.NoError(t, repo.SeedDefaults(ctx, template.Defaults()), "re-seeding keeps edited rows")
	got, err = repo.Get(ctx, template.TypePasswordChanged, template.ChannelEmail)
	require.NoError(t, err)
	require.Equal(t, "Custom subject", got.Subject)

	_, err = repo.Get(ctx, "missing.type", template.ChannelWeb)
	require.ErrorIs(t, err, template.ErrNotFound)

	err = repo.Save(ctx, template.Template{
		Type: template.TypePasswordReset, Channel: template.ChannelSSE,
		Title: "t", Preview: "{{.Token}}", Event: "notification.created",
	})
	require.Error(t, err, "token outside the email body is rejected")
}
