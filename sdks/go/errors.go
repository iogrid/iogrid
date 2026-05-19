package iogrid

import (
	"fmt"
	"strconv"
)

// Error is the canonical error type returned by every SDK method when
// the iogrid API responds with a non-2xx status. The Code field is the
// stable machine-readable error code (mirrors
// iogrid.common.v1.ErrorCode); callers should switch on this rather
// than parsing the human-readable message.
//
// Network failures (DNS, connect, abort) bubble up the underlying error
// from net/http and are NOT wrapped — they have no server-side envelope
// to attach.
type Error struct {
	Status    int
	Code      string
	Message   string
	FieldPath string
	Metadata  map[string]string
	RequestID string
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("iogrid: %s (status=%d, code=%s, requestID=%s)",
			e.Message, e.Status, e.Code, e.RequestID)
	}
	return fmt.Sprintf("iogrid: %s (status=%d, code=%s)", e.Message, e.Status, e.Code)
}

// Common error codes — provided as untyped constants so callers can
// switch on err.Code directly without importing a separate enum.
const (
	ErrCodeInvalidArgument          = "INVALID_ARGUMENT"
	ErrCodeNotFound                 = "NOT_FOUND"
	ErrCodeAlreadyExists            = "ALREADY_EXISTS"
	ErrCodePermissionDenied         = "PERMISSION_DENIED"
	ErrCodeUnauthenticated          = "UNAUTHENTICATED"
	ErrCodeResourceExhausted        = "RESOURCE_EXHAUSTED"
	ErrCodeFailedPrecondition       = "FAILED_PRECONDITION"
	ErrCodeInternal                 = "INTERNAL"
	ErrCodeUnavailable              = "UNAVAILABLE"
	ErrCodeDeadlineExceeded         = "DEADLINE_EXCEEDED"
	ErrCodeAbuseBlocked             = "ABUSE_BLOCKED"
	ErrCodeAbuseRateLimited         = "ABUSE_RATE_LIMITED"
	ErrCodeAbuseCategoryDisallowed  = "ABUSE_CATEGORY_DISALLOWED"
	ErrCodeAbuseDestinationBlocked  = "ABUSE_DESTINATION_BLOCKED"
	ErrCodeStepUpRequired           = "STEP_UP_REQUIRED"
	ErrCodeBillingPastDue           = "BILLING_PAST_DUE"
)

// RetryAfterSeconds reports the server-suggested retry delay (seconds)
// extracted from an *Error's metadata. Returns (0, false) when the
// error is not an *Error or no hint was present.
func RetryAfterSeconds(err error) (int, bool) {
	e, ok := err.(*Error)
	if !ok {
		return 0, false
	}
	raw := e.Metadata["retry_after_seconds"]
	if raw == "" {
		raw = e.Metadata["retryAfterSeconds"]
	}
	if raw == "" {
		return 0, false
	}
	n, perr := strconv.Atoi(raw)
	if perr != nil {
		return 0, false
	}
	return n, true
}
