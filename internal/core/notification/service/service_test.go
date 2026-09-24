package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/notification/ports"
	notificationservice "github.com/turahe/blog-api/internal/core/notification/service"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type captureMailer struct {
	messages []ports.Message
}

func (c *captureMailer) Send(_ context.Context, msg ports.Message) error {
	c.messages = append(c.messages, msg)
	return nil
}

func TestPasswordResetIncludesTokenOnlyInBody(t *testing.T) {
	t.Parallel()

	mailer := &captureMailer{}
	svc := notificationservice.New(mailer, nil, "http://127.0.0.1:8080")
	user := userdomain.User{Email: "ada@example.com", FullName: "Ada"}

	svc.PasswordReset(context.Background(), user, "secret-token", time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))

	require.Len(t, mailer.messages, 1)
	require.Equal(t, "ada@example.com", mailer.messages[0].To)
	require.Equal(t, "Reset your password", mailer.messages[0].Subject)
	require.Contains(t, mailer.messages[0].Text, "secret-token")
	require.NotContains(t, mailer.messages[0].Subject, "secret-token")
}
