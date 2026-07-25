package apierr

import (
	"encoding/json"
	"net/http"
)

// Reason codes from Section 8 of the implementation plan.
// Every service must use these stable, machine-readable strings.
const (
	TokenInvalid          = "TOKEN_INVALID"
	EpochStale            = "EPOCH_STALE"
	ContainmentHalt       = "CONTAINMENT_HALT"
	MandateScopeViolation = "MANDATE_SCOPE_VIOLATION"
	MandateExpired        = "MANDATE_EXPIRED"
	MandateRevoked        = "MANDATE_REVOKED"
	CaveatWidening        = "CAVEAT_WIDENING"
	PolicyDenied          = "POLICY_DENIED"
	VelocityExceeded      = "VELOCITY_EXCEEDED"
	BudgetExceeded        = "BUDGET_EXCEEDED"
	RailError             = "RAIL_ERROR"
	ApprovalDenied        = "APPROVAL_DENIED"
	ApprovalVoided        = "APPROVAL_VOIDED"
)

// Error is the error object inside the standard response envelope.
type Error struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// Envelope is the standard error response shape for every built service.
// Section 4.4 of the implementation plan.
type Envelope struct {
	Error Error `json:"error"`
}

// New creates a new error envelope.
func New(code, message string, details ...map[string]interface{}) *Envelope {
	e := &Envelope{
		Error: Error{
			Code:    code,
			Message: message,
		},
	}
	if len(details) > 0 && details[0] != nil {
		e.Error.Details = details[0]
	}
	return e
}

// Write writes the error envelope as JSON with the given HTTP status code.
func Write(w http.ResponseWriter, status int, e *Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(e)
}

// WriteError is a convenience function that creates and writes an envelope in one call.
func WriteError(w http.ResponseWriter, status int, code, message string, details ...map[string]interface{}) {
	Write(w, status, New(code, message, details...))
}
