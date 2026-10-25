package handler

import (
	"errors"

	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

// Logging policy
//
// Backend logs exist to operate the service, not to read anyone's
// bookkeeping. A log line may carry: ids (user, session, file, mutation, bank
// line), HTTP statuses, byte lengths and counts, Claude stop reasons, Redis
// key patterns and error classes. It must never carry: response bodies,
// Claude output (not even a preview), supplier names, amounts, descriptions,
// file names, e-mail addresses, tokens or keys.
//
// Errors from e-boekhouden may embed upstream text (the capped envelope
// message can echo the user's own input), so they are logged through errClass
// instead of %v. The eboekhouden client never puts a response body in an
// error (eboekhouden.APIError), and the error of a request whose path carries
// user input (relation search, KvK search) is not logged at all. Internal
// errors (Redis, JSON decoding, our own sentinels) are logged with %v.
//
// The frontend has the same rule for analytics: see analytics.ts.

// errClass renders an error for a log line without upstream text: the
// session-expired sentinel by name, an e-boekhouden API error by path, status,
// code and length, anything else by its message.
func errClass(err error) string {
	if err == nil {
		return "<nil>"
	}
	if errors.Is(err, eboekhouden.ErrSessionExpired) {
		return "session_expired"
	}
	var api *eboekhouden.APIError
	if errors.As(err, &api) {
		return api.LogString()
	}
	return err.Error()
}
