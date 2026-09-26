package newsletterprovider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/resend/resend-go/v3"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/mail"
	"github.com/turahe/blog-api/internal/adapters/outbound/newsletterprovider"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

type resendClient struct {
	req            *resend.SendEmailRequest
	idempotencyKey string
	err            error
}

func (c *resendClient) SendEmail(_ context.Context, req *resend.SendEmailRequest, idempotencyKey string) error {
	c.req, c.idempotencyKey = req, idempotencyKey
	return c.err
}

func (*resendClient) From() string { return "Blog <mailer@example.test>" }

func TestResendSendsIssueWithUnsubscribeHeaders(t *testing.T) {
	t.Parallel()

	client := &resendClient{}
	msg := email()
	require.NoError(t, newsletterprovider.NewResend(client).Send(t.Context(), msg))

	require.Equal(t, msg.IdempotencyKey, client.idempotencyKey)
	require.Equal(t, &resend.SendEmailRequest{
		From: `"Turahe Blog" <news@example.test>`, To: []string{"reader@example.test"}, Subject: "Hello ünïcode",
		Text: "Plain body", Html: "<p>HTML body</p>", ReplyTo: "hello@example.test", Headers: msg.Headers,
	}, client.req)
}

func TestResendFallsBackToMailerSender(t *testing.T) {
	t.Parallel()

	client := &resendClient{}
	msg := email()
	msg.FromName, msg.FromEmail, msg.HTML = "", "", ""
	require.NoError(t, newsletterprovider.NewResend(client).Send(t.Context(), msg))

	require.Equal(t, `"Blog" <mailer@example.test>`, client.req.From)
	require.Empty(t, client.req.Html)
}

func TestResendClassifiesFailures(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err       error
		permanent bool
	}{
		"rejected":     {&mail.ResendError{Status: 422, Err: errors.New("invalid to")}, true},
		"rate limited": {&mail.ResendError{Status: 429, Err: errors.New("slow down")}, false},
		"unavailable":  {&mail.ResendError{Status: 503, Err: errors.New("down")}, false},
		"no response":  {&mail.ResendError{Err: errors.New("dial tcp: timeout")}, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := newsletterprovider.NewResend(&resendClient{err: tc.err}).Send(t.Context(), email())
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.permanent, errors.Is(err, domain.ErrPermanent))
		})
	}
}

func TestResendRejectsBadInputWithoutCalling(t *testing.T) {
	t.Parallel()

	badRecipient := email()
	badRecipient.To = "not an address"

	badHeader := email()
	badHeader.Headers = map[string]string{"List-Unsubscribe": "<https://x.test>\r\nBcc: evil@example.test"}

	for name, msg := range map[string]ports.Email{"recipient": badRecipient, "header": badHeader} {
		client := &resendClient{}
		require.ErrorIs(t, newsletterprovider.NewResend(client).Send(t.Context(), msg), domain.ErrPermanent, name)
		require.Nil(t, client.req, name)
	}
}
