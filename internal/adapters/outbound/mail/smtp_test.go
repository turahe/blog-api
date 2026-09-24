package mail_test

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/mail"
	"github.com/turahe/blog-api/internal/core/notification/ports"
)

func TestSend(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	got := make(chan string, 1)
	go serveSMTP(ln, got)

	host, portText, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	sender, err := mail.NewSMTP(host, port, "", "", "Blog <blog@localhost>")
	require.NoError(t, err)

	err = sender.Send(context.Background(), ports.Message{
		To: "ada@example.com", Subject: "Reset your password", Text: "token stays in the body",
	})
	require.NoError(t, err)

	body := <-got
	require.Contains(t, body, "Subject: Reset your password")
	require.Contains(t, body, "To: ada@example.com")
	require.Contains(t, body, "token stays in the body")
}

func TestRejectsHeaderInjection(t *testing.T) {
	t.Parallel()

	sender, err := mail.NewSMTP("127.0.0.1", 1025, "", "", "blog@localhost")
	require.NoError(t, err)

	err = sender.Send(context.Background(), ports.Message{
		To: "ada@example.com", Subject: "hello\r\nBcc: evil@example.com", Text: "no",
	})
	require.Error(t, err)
}

func serveSMTP(ln net.Listener, got chan<- string) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	_, _ = conn.Write([]byte("220 mailpit test\r\n"))

	var data strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			_, _ = conn.Write([]byte("250 hello\r\n"))
		case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
			_, _ = conn.Write([]byte("250 ok\r\n"))
		case cmd == "DATA":
			_, _ = conn.Write([]byte("354 go\r\n"))
			for {
				row, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(row, "\r\n") == "." {
					break
				}
				data.WriteString(row)
			}
			_, _ = conn.Write([]byte("250 queued\r\n"))
			got <- data.String()
		case cmd == "QUIT":
			_, _ = conn.Write([]byte("221 bye\r\n"))
			return
		default:
			_, _ = conn.Write([]byte("250 ok\r\n"))
		}
	}
}
