package mail

import (
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
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
