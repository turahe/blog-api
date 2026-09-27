package mail_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/mail"
	"github.com/turahe/blog-api/internal/core/notification/ports"
)

func TestNewSMTPValidatesSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		host    string
		port    int
		from    string
		wantErr string
	}{
		{name: "blank host", host: " ", port: 25, from: "blog@localhost", wantErr: "host is required"},
		{name: "port zero", host: "localhost", port: 0, from: "blog@localhost", wantErr: "port out of range"},
		{name: "port too large", host: "localhost", port: 65536, from: "blog@localhost", wantErr: "port out of range"},
		{name: "invalid from", host: "localhost", port: 25, from: "not an address", wantErr: "from address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sender, err := mail.NewSMTP(tt.host, tt.port, "", "", tt.from)
			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, sender)
		})
	}
}

func TestSend(t *testing.T) {
	t.Parallel()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
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

func TestSendMultipartWhenHTMLIsSet(t *testing.T) {
	t.Parallel()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
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
		To: "ada@example.com", Subject: "Reset your password",
		Text: "plain token", HTML: `<p style="margin:0">html token</p>`,
	})
	require.NoError(t, err)

	msg, err := netmail.ReadMessage(strings.NewReader(<-got))
	require.NoError(t, err)

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/alternative", mediaType)

	reader := multipart.NewReader(msg.Body, params["boundary"])

	var parts []string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)

		body, err := io.ReadAll(part)
		require.NoError(t, err)

		parts = append(parts, part.Header.Get("Content-Type")+"|"+string(body))
	}

	require.Equal(t, []string{
		"text/plain; charset=UTF-8|plain token",
		`text/html; charset=UTF-8|<p style="margin:0">html token</p>`,
	}, parts)
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

func TestSendRejectsInvalidRecipient(t *testing.T) {
	t.Parallel()

	sender, err := mail.NewSMTP("127.0.0.1", 1025, "", "", "blog@localhost")
	require.NoError(t, err)

	err = sender.Send(t.Context(), ports.Message{To: "not an address", Subject: "hi", Text: "no"})
	require.ErrorContains(t, err, "recipient")
}

func TestSMTPFrom(t *testing.T) {
	t.Parallel()

	sender, err := mail.NewSMTP("localhost", 25, "", "", " Blog <blog@localhost> ")
	require.NoError(t, err)
	require.Equal(t, "Blog <blog@localhost>", sender.From())
}

func TestSendRawDialFailure(t *testing.T) {
	t.Parallel()

	sender, err := mail.NewSMTP("127.0.0.1", 1025, "", "", "blog@localhost")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err = sender.SendRaw(ctx, "ada@example.com", []byte("Subject: x\r\n\r\nbody"))
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "smtp dial")
}

func TestSendRawReportsProtocolFailures(t *testing.T) {
	t.Parallel()

	const authEHLO = "250-hello\r\n250 AUTH PLAIN"

	tests := []struct {
		name     string
		username string
		script   smtpScript
		wantErr  string
	}{
		{name: "greeting rejected", script: smtpScript{greeting: "554 go away"}, wantErr: "smtp client"},
		{name: "auth rejected", username: "user", script: smtpScript{ehlo: authEHLO, replies: map[string]string{"AUTH": "535 bad credentials"}}, wantErr: "smtp auth"},
		{name: "sender rejected", script: smtpScript{replies: map[string]string{"MAIL": "550 no sender"}}, wantErr: "smtp mail from"},
		{name: "recipient rejected", script: smtpScript{replies: map[string]string{"RCPT": "550 no mailbox"}}, wantErr: "smtp rcpt"},
		{name: "data rejected", script: smtpScript{replies: map[string]string{"DATA": "554 no data"}}, wantErr: "smtp data"},
		{name: "message rejected", script: smtpScript{afterData: "554 spam"}, wantErr: "smtp finish"},
		{name: "authenticated delivery", username: "user", script: smtpScript{ehlo: authEHLO}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			host, port := startSMTP(t, tt.script)

			sender, err := mail.NewSMTP(host, port, tt.username, "secret", "blog@localhost")
			require.NoError(t, err)

			err = sender.SendRaw(t.Context(), "ada@example.com", []byte("Subject: x\r\n\r\nbody"))
			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestSendRawReportsWriteFailure(t *testing.T) {
	t.Parallel()

	host, port := startSMTP(t, smtpScript{closeOnData: true})

	sender, err := mail.NewSMTP(host, port, "", "", "blog@localhost")
	require.NoError(t, err)

	body := []byte(strings.Repeat("x", 8<<20))

	err = sender.SendRaw(t.Context(), "ada@example.com", body)
	require.ErrorContains(t, err, "smtp write")
}

// smtpScript configures startSMTP's replies; empty fields answer with success.
type smtpScript struct {
	greeting    string
	ehlo        string
	replies     map[string]string
	afterData   string
	closeOnData bool
}

func startSMTP(t *testing.T, script smtpScript) (string, int) {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go serveScript(ln, script)

	host, portText, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	return host, port
}

func serveScript(ln net.Listener, script smtpScript) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	reply := func(line, fallback string) {
		if line == "" {
			line = fallback
		}

		_, _ = conn.Write([]byte(line + "\r\n"))
	}

	reply(script.greeting, "220 test")

	reader := bufio.NewReader(conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		verb, _, _ := strings.Cut(strings.ToUpper(strings.TrimSpace(line)), " ")
		switch verb {
		case "EHLO", "HELO":
			reply(script.ehlo, "250 hello")
		case "QUIT":
			reply("", "221 bye")

			return
		case "DATA":
			if r, ok := script.replies["DATA"]; ok {
				reply(r, "")

				continue
			}

			reply("", "354 go")

			if script.closeOnData {
				return
			}

			for {
				row, err := reader.ReadString('\n')
				if err != nil {
					return
				}

				if strings.TrimRight(row, "\r\n") == "." {
					break
				}
			}

			reply(script.afterData, "250 queued")
		case "AUTH":
			reply(script.replies[verb], "235 ok")
		default:
			reply(script.replies[verb], "250 ok")
		}
	}
}

func serveSMTP(ln net.Listener, got chan<- string) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

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
