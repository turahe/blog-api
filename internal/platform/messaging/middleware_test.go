package messaging

import (
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/require"
)

func TestRecovererConvertsPanicToError(t *testing.T) {
	t.Parallel()

	handler := Recoverer(func(*message.Message) ([]*message.Message, error) {
		panic("boom")
	})

	msgs, err := handler(message.NewMessage("id-1", nil))
	require.Nil(t, msgs)
	require.ErrorContains(t, err, "message handler panic: boom")
}
