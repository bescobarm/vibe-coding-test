package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
)

const fieldAmountMinor = "amount_minor"

var (
	errMalformedEvent = errors.New("lifecycle event is not valid JSON")
	errInvalidEvent   = errors.New("lifecycle event has invalid fields")
	errEventFailed    = errors.New("lifecycle event could not be applied")

	msgAmountNotAllowed = "not allowed: expiry releases the whole hold"
)

// amountRules says, per event type, whether amount_minor is required, optional or not allowed.
var (
	amountRequired = map[string]bool{lifecycle.TypeIncrement: true, lifecycle.TypeCapture: true}
	amountRefused  = map[string]bool{lifecycle.TypeExpiry: true}
)

// lifecycleRequest is the body of every lifecycle call. Without amount_minor a
// reversal releases the whole hold and a refund refunds what is left.
type lifecycleRequest struct {
	EventID     string `json:"event_id"`
	AmountMinor *int64 `json:"amount_minor,omitempty"`
}

func (r lifecycleRequest) validate(eventType string) map[string]string {
	errs := map[string]string{}
	if r.EventID == "" {
		errs["event_id"] = domain.MsgRequired
	}

	switch {
	case r.AmountMinor == nil && amountRequired[eventType]:
		errs[fieldAmountMinor] = domain.MsgRequired
	case r.AmountMinor == nil:
	case amountRefused[eventType]:
		errs[fieldAmountMinor] = msgAmountNotAllowed
	case *r.AmountMinor <= 0:
		errs[fieldAmountMinor] = domain.MsgPositive
	}

	return errs
}

// lifecycleConflicts are refusals the client can fix; each message is returned as is.
var lifecycleConflicts = []error{
	lifecycle.ErrDeclined, lifecycle.ErrNotPending, lifecycle.ErrNotCaptured,
	lifecycle.ErrOverHold, lifecycle.ErrOverRefund, lifecycle.ErrEventConflict,
}

func (h *handler) handleLifecycleEvent(eventType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req lifecycleRequest

		err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req)
		if err != nil {
			h.writeBodyError(w, err, errMalformedEvent)
			return
		}

		errs := req.validate(eventType)
		if len(errs) > 0 {
			h.writeJSON(w, http.StatusBadRequest, apiError{Error: errInvalidEvent.Error(), Fields: errs})
			return
		}

		ev := lifecycle.Event{ID: req.EventID, AuthorizationID: r.PathValue(pathID), Type: eventType, AmountMinor: req.AmountMinor}

		res, replayed, err := h.Lifecycle.Apply(r.Context(), ev)
		if err != nil {
			h.writeLifecycleError(w, ev, err)
			return
		}

		h.log.Info("lifecycle", "authorization_id", ev.AuthorizationID, "event_id", ev.ID, "type", eventType,
			"status", res.Status, "decision", res.Decision, "held_minor", res.HeldMinor, "replayed", replayed)

		if replayed {
			w.Header().Set(headerReplay, "true")
		}

		h.writeJSON(w, http.StatusOK, res)
	}
}

func (h *handler) writeLifecycleError(w http.ResponseWriter, ev lifecycle.Event, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		h.writeError(w, http.StatusNotFound, errAuthorizationNotFound)
		return
	}

	for _, conflict := range lifecycleConflicts {
		if errors.Is(err, conflict) {
			h.writeError(w, http.StatusConflict, conflict)
			return
		}
	}

	h.log.Error(errEventFailed.Error(), "authorization_id", ev.AuthorizationID, "event_id", ev.ID, "err", err)
	h.writeError(w, http.StatusInternalServerError, errEventFailed)
}
