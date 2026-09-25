package mailqueue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	"github.com/turahe/blog-api/internal/core/notification/ports"
	"github.com/turahe/blog-api/internal/platform/messaging"
	"github.com/turahe/blog-api/internal/platform/security/secretbox"
)

type recordingMailer struct {
	sent []ports.Message
	err  error
}

func (m *recordingMailer) Send(_ context.Context, msg ports.Message) error {
	if m.err != nil {
		return m.err
	}

	m.sent = append(m.sent, msg)

	return nil
}

func newBox(t *testing.T) *secretbox.Box {
	t.Helper()

	box, err := secretbox.New(make([]byte, 32))
	require.NoError(t, err)

	return box
}

func TestMailerQueuesEncryptedCommandThatHandlerSends(t *testing.T) {
	t.Parallel()

	box := newBox(t)
	recorder := &eventtest.Recorder{}
	fallback := &recordingMailer{}
	email := ports.Message{To: "reader@example.com", Subject: "Reset your password", Text: "token raw-reset-token"}

	require.NoError(t, New(recorder, box, fallback, nil).Send(t.Context(), email))
	require.Empty(t, fallback.sent)

	events := recorder.Events()
	require.Len(t, events, 1)
	require.Equal(t, event.NotificationEmailRequested, events[0].Type)
	require.Equal(t, event.AggregateEmail, events[0].AggregateType)

	payload, err := json.Marshal(events[0].Payload)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "raw-reset-token")
	require.NotContains(t, string(payload), "reader@example.com")

	sender := &recordingMailer{}
	require.NoError(t, Handler(box, sender, slog.New(slog.DiscardHandler))(message.NewMessage(events[0].ID.String(), payload)))
	require.Equal(t, []ports.Message{email}, sender.sent)
}

func TestMailerSendsInlineWhenCommandCannotBeStored(t *testing.T) {
	t.Parallel()

	fallback := &recordingMailer{}
	email := ports.Message{To: "reader@example.com", Subject: "Hi", Text: "body"}

	mailer := New(&eventtest.Recorder{Err: errors.New("db down")}, newBox(t), fallback, nil)
	require.NoError(t, mailer.Send(t.Context(), email))
	require.Equal(t, []ports.Message{email}, fallback.sent)
}

func TestHandlerRejectsMalformedCommandsPermanently(t *testing.T) {
	t.Parallel()

	box := newBox(t)
	other, err := secretbox.New([]byte("another-master-key-of-32-bytes!!"))
	require.NoError(t, err)

	foreign, err := other.Encrypt([]byte(`{"to":"a@example.com"}`))
	require.NoError(t, err)

	empty, err := box.Encrypt([]byte(`{"subject":"no recipient"}`))
	require.NoError(t, err)

	for name, payload := range map[string]string{
		"not json":      `nope`,
		"no ciphertext": `{}`,
		"wrong key":     `{"ciphertext":"` + foreign + `"}`,
		"no recipient":  `{"ciphertext":"` + empty + `"}`,
	} {
		err := Handler(box, &recordingMailer{}, slog.New(slog.DiscardHandler))(message.NewMessage("m", []byte(payload)))
		require.ErrorIs(t, err, messaging.ErrPermanent, name)
	}
}

func TestHandlerReturnsRetryableSendError(t *testing.T) {
	t.Parallel()

	box := newBox(t)
	sealedEmail, err := box.Encrypt([]byte(`{"to":"a@example.com","subject":"s","text":"t"}`))
	require.NoError(t, err)

	smtpErr := errors.New("smtp rcpt: 450 <a@example.com>: mailbox busy")
	err = Handler(box, &recordingMailer{err: smtpErr}, slog.New(slog.DiscardHandler))(
		message.NewMessage("m", []byte(`{"ciphertext":"`+sealedEmail+`"}`)))
	require.ErrorIs(t, err, ErrSendFailed)
	require.NotErrorIs(t, err, messaging.ErrPermanent)
	require.NotContains(t, err.Error(), "a@example.com", "recipient must not reach dead-letter headers")
}
