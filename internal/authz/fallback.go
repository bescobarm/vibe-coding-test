package authz

import (
	"context"
	"fmt"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

const (
	msgDefaultPolicy  = "The company has no policy; the default policy applies"
	msgDefaultUnknown = "The card or its company's policy could not be loaded; the default policy applies"
	msgFailClosed     = "Fallback fail_closed: a dependency failed or the deadline passed"
	fmtFailOpenOK     = "Fallback fail_open_limited: approved up to %s"
	fmtFailOpenOver   = "Fallback fail_open_limited: amount %s is over %s"
	fmtFailOpenOtherC = "Fallback fail_open_limited: limit is in %s, request is in %s"
)

// BuildFallback is the decision when the engine can't decide normally. It is a
// pure function of the request and the policy, so a retry gets the same
// answer even when nothing could be stored (D-062). fail_open_limited approves
// up to the cap in its currency (D-061); anything else declines.
func BuildFallback(req domain.AuthRequest, doc policy.Document, version int) domain.AuthResponse {
	decision, msg := decideFallback(req, doc.Fallback)

	resp := domain.AuthResponse{
		AuthorizationID: req.AuthorizationID,
		Decision:        decision,
		Reasons:         []domain.Reason{{Code: domain.ReasonFallbackApplied, Message: msg}},
		PolicyVersion:   version,
		FallbackUsed:    true,
	}

	if doc.CompanyID == policy.DefaultCompanyID {
		resp.Reasons = append(resp.Reasons, domain.Reason{Code: domain.ReasonDefaultPolicy, Message: msgDefaultUnknown})
	}

	if decision == domain.Approve {
		resp.ApprovedAmountMinor = req.AmountMinor
	}

	return resp
}

func decideFallback(req domain.AuthRequest, f *policy.Fallback) (domain.Decision, string) {
	if f == nil || f.Mode != policy.FailOpenLimited || f.MaxAmount == nil {
		return domain.Decline, msgFailClosed
	}

	limit := f.MaxAmount

	switch {
	case req.Currency != limit.Currency:
		return domain.Decline, fmt.Sprintf(fmtFailOpenOtherC, limit.Currency, req.Currency)
	case req.AmountMinor > limit.AmountMinor:
		return domain.Decline, fmt.Sprintf(fmtFailOpenOver, domain.FormatMoney(req.AmountMinor, req.Currency),
			domain.FormatMoney(limit.AmountMinor, limit.Currency))
	default:
		return domain.Approve, fmt.Sprintf(fmtFailOpenOK, domain.FormatMoney(limit.AmountMinor, limit.Currency))
	}
}

// getFallbackPolicy is the company's policy when it was loaded, otherwise the
// default: an unknown company fails closed (D-062).
func getFallbackPolicy(e *pipeline.Evaluation) (policy.Document, int) {
	if e.Policy.CompanyID == "" {
		return policy.GetDefault(), 0
	}

	return e.Policy, e.PolicyVersion
}

// answerFallback answers after a dependency failed. Nothing else is running for this
// request, so the decision is stored right away, with its own timeout because
// the request's context may have expired (D-031). If the store already holds
// an answer for this authorization_id, that answer wins.
func (s *Service) answerFallback(ctx context.Context, req domain.AuthRequest, e *pipeline.Evaluation, start time.Time, cause error) domain.AuthResponse {
	resp := s.buildFallbackResponse(req, e, start, cause)

	stored, ok := s.storeFallback(ctx, req, resp)
	if ok {
		return stored
	}

	return resp
}

// answerLateFallback answers when the deadline passed while the decision was still
// running (D-064). That decision holds the claim on the authorization_id, so
// the fallback is stored in the background once it ends.
func (s *Service) answerLateFallback(ctx context.Context, req domain.AuthRequest, e *pipeline.Evaluation, start time.Time, pending <-chan outcome) domain.AuthResponse {
	resp := s.buildFallbackResponse(req, e, start, ctx.Err())

	s.background.Go(func() {
		s.storeAfterDecision(ctx, req, resp, pending)
	})

	return resp
}

func (s *Service) buildFallbackResponse(req domain.AuthRequest, e *pipeline.Evaluation, start time.Time, cause error) domain.AuthResponse {
	doc, version := getFallbackPolicy(e)
	resp := BuildFallback(req, doc, version)
	resp.LatencyMS = roundToMS(s.now().Sub(start))

	s.log.Warn("fallback applied",
		logKeyAuthorizationID, req.AuthorizationID,
		logKeyCompanyID, e.Card.CompanyID,
		logKeyDecision, resp.Decision,
		"policy", doc.CompanyID,
		"cause", cause.Error(),
	)

	return resp
}

func (s *Service) storeAfterDecision(ctx context.Context, req domain.AuthRequest, resp domain.AuthResponse, pending <-chan outcome) {
	timer := time.NewTimer(slowDecisionWait)
	defer timer.Stop()

	select {
	case result := <-pending:
		if result.err == nil {
			s.log.Error("decision committed after the deadline; the processor got the fallback",
				logKeyAuthorizationID, req.AuthorizationID, "stored", result.resp.Decision, "answered", resp.Decision)

			return
		}
	case <-timer.C:
		s.log.Error("fallback not stored: the late decision is still running", logKeyAuthorizationID, req.AuthorizationID)
		return
	}

	s.storeFallback(ctx, req, resp)
}

// storeFallback records the fallback like any decision. When Postgres is down
// it can't, and the decision is logged instead (D-062). ok reports that the
// store answered, with this fallback or with an answer stored earlier.
func (s *Service) storeFallback(ctx context.Context, req domain.AuthRequest, resp domain.AuthResponse) (domain.AuthResponse, bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fallbackWriteTimeout)
	defer cancel()

	stored, _, err := s.recorder.Decide(ctx, req, func(context.Context) (domain.AuthResponse, error) {
		return resp, nil
	})
	if err != nil {
		s.log.Error("fallback not stored",
			logKeyAuthorizationID, req.AuthorizationID,
			logKeyDecision, resp.Decision,
			"card_id", req.CardID,
			"amount_minor", req.AmountMinor,
			"currency", req.Currency,
			"err", err.Error(),
		)

		return domain.AuthResponse{}, false
	}

	s.doorbell()

	return stored, true
}
