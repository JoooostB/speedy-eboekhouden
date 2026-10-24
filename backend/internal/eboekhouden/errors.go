package eboekhouden

import (
	"fmt"
	"strings"
)

// APIError is an e-boekhouden response that is not a success: an error
// envelope (HTTP 200 with errorType) or a non-2xx status. It never carries
// the response body. Message is the capped envelope message: it is meant for
// the user and may echo the user's own input (a relation name, a file name),
// so Error() includes it and LogString() leaves it out. Handlers log
// e-boekhouden errors through errClass (handler/logging.go), never with %v.
type APIError struct {
	// Path is the request path without origin and without query string; a
	// relation search carries the supplier name in its query.
	Path string
	// Status is the HTTP status; 200 for an error envelope.
	Status int
	// Code is the envelope code such as "VAL_001"; "" for a bare status.
	Code string
	// Message is the capped envelope message; "" for a bare status.
	Message string
	// BodyLen is the response length in bytes.
	BodyLen int
}

// Error keeps the two shapes callers and users have always seen:
// "e-Boekhouden: <message>" for an envelope, "request to <path> returned
// <status>" for a bare status.
func (e *APIError) Error() string {
	if e.Message != "" {
		return "e-Boekhouden: " + e.Message
	}
	return fmt.Sprintf("request to %s returned %d (%d bytes)", e.Path, e.Status, e.BodyLen)
}

// LogString describes the error for a log line: path, status, code and
// length, never the message.
func (e *APIError) LogString() string {
	return fmt.Sprintf("eboekhouden path=%s status=%d code=%q bodyLen=%d", e.Path, e.Status, e.Code, e.BodyLen)
}

// newEnvelopeError builds the APIError for an error envelope.
func newEnvelopeError(path string, status int, env errorEnvelope, bodyLen int) *APIError {
	return &APIError{Path: pathOnly(path), Status: status, Code: env.Code, Message: envelopeMessage(env), BodyLen: bodyLen}
}

// newStatusError builds the APIError for a non-2xx status without envelope.
func newStatusError(path string, status, bodyLen int) *APIError {
	return &APIError{Path: pathOnly(path), Status: status, BodyLen: bodyLen}
}

// pathOnly strips the query string from a request path.
func pathOnly(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}
