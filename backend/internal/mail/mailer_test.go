package mail

import (
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "dial tcp: i/o timeout to iemand@voorbeeld.nl" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestErrClassNeverCarriesTheRecipient(t *testing.T) {
	const recipient = "iemand@voorbeeld.nl"
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "<nil>"},
		{"smtp reply", &textproto.Error{Code: 550, Msg: "5.1.1 <" + recipient + ">: Recipient address rejected"}, "smtp_reply_550"},
		{"wrapped smtp reply", fmt.Errorf("sending: %w", &textproto.Error{Code: 451, Msg: recipient}), "smtp_reply_451"},
		{"dial error", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, "smtp_network"},
		{"timeout", timeoutErr{}, "smtp_timeout"},
		{"other", errors.New("smtp: server doesn't support AUTH for " + recipient), "smtp_error(*errors.errorString)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ErrClass(tc.err)
			if got != tc.want {
				t.Errorf("ErrClass() = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, recipient) {
				t.Errorf("ErrClass() = %q leaks the recipient", got)
			}
		})
	}
}

// fakeSMTP is a minimal SMTP server without STARTTLS and AUTH. It records
// the envelope and the message of one delivery.
func fakeSMTP(t *testing.T) (host, port string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		tp := textproto.NewConn(conn)
		var envelope []string
		tp.PrintfLine("220 voorbeeld.test ESMTP")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			switch cmd := strings.ToUpper(strings.Fields(line + " x")[0]); cmd {
			case "EHLO", "HELO":
				tp.PrintfLine("250 voorbeeld.test")
			case "MAIL", "RCPT":
				envelope = append(envelope, line)
				tp.PrintfLine("250 OK")
			case "DATA":
				tp.PrintfLine("354 Go ahead")
				data, _ := tp.ReadDotLines()
				got <- strings.Join(envelope, "\n") + "\n" + strings.Join(data, "\n")
				tp.PrintfLine("250 OK")
			case "QUIT":
				tp.PrintfLine("221 Bye")
				return
			default:
				tp.PrintfLine("502 Unknown")
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	return h, p, got
}

func TestDeliverSendsTheMessage(t *testing.T) {
	host, port, got := fakeSMTP(t)
	m := New(Config{Host: host, Port: port, From: "noreply@voorbeeld.test"})

	if err := m.deliver("iemand@voorbeeld.test", []byte("Subject: Test\r\n\r\nHallo\r\n")); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	msg := <-got
	for _, want := range []string{"MAIL FROM:<noreply@voorbeeld.test>", "RCPT TO:<iemand@voorbeeld.test>", "Subject: Test", "Hallo"} {
		if !strings.Contains(msg, want) {
			t.Errorf("delivered message lacks %q:\n%s", want, msg)
		}
	}
}

func TestDeliverRefusesCredentialsWithoutAuth(t *testing.T) {
	host, port, _ := fakeSMTP(t)
	m := New(Config{Host: host, Port: port, Username: "speedy", Password: "geheim", From: "noreply@voorbeeld.test"})
	if err := m.deliver("iemand@voorbeeld.test", []byte("x")); err == nil {
		t.Fatal("deliver sent with credentials to a server without AUTH")
	}
}

// The bug: smtp.SendMail has no timeout, so a server that accepts and then
// says nothing held the sender forever.
func TestDeliverGivesUpOnAStalledServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			time.Sleep(5 * time.Second)
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	m := New(Config{Host: host, Port: port, From: "noreply@voorbeeld.test"})
	m.sendTimeout = 200 * time.Millisecond

	start := time.Now()
	err = m.deliver("iemand@voorbeeld.test", []byte("x"))
	if err == nil {
		t.Fatal("deliver succeeded against a silent server")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("deliver took %v, want it bounded by sendTimeout", elapsed)
	}
	if got := ErrClass(err); got != "smtp_timeout" {
		t.Errorf("ErrClass = %q, want smtp_timeout", got)
	}
}
