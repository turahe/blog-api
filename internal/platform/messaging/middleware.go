package messaging

import (
	"fmt"
	"runtime/debug"

	"github.com/ThreeDotsLabs/watermill/message"
)

// Recoverer turns a message handler panic into an error so the router
// nacks the message instead of crashing the worker process.
func Recoverer(h message.HandlerFunc) message.HandlerFunc {
	return func(msg *message.Message) (msgs []*message.Message, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("message handler panic: %v\n%s", recovered, debug.Stack())
			}
		}()

		return h(msg)
	}
}
