package simulator

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
)

// Amounts in minor units and other scenario parameters (README §15).
const (
	currency       = "USD"
	countryHome    = "US"
	countryAbroad  = "BR"
	entryChip      = "chip"
	entryWallet    = "wallet"
	entryOnline    = "ecommerce"
	entryTap       = "contactless"
	pathIncrement  = "increment"
	expectDecline  = "decline"
	merchantSaaS   = "m_saas"
	oddCents       = 37 // keeps amounts off round hundreds, so ROUND_AMOUNT stays quiet on normal traffic
	normalRequests = 8

	lunchMinor, lunchSpread     = 1500, 400
	softwareMinor               = 4900
	airlineMinor, airlineSpread = 32000, 4000
	taxiMinor, taxiSpread       = 2200, 500

	cardTestingAttempts = 8
	cardTestingMinMinor = 100
	cardTestingSpread   = 400

	splitParts       = 3
	splitMinor       = 49000
	splitSpread      = 900
	stolenHistory    = 6
	stolenUsualMinor = 4500
	stolenSpread     = 300
	stolenMinor      = 30000
	stolenAttackPad  = 999
	gamblingMinor    = 5000
	hotelMinor       = 30000
	hotelExtraMinor  = 15000
)

// Merchants and MCCs the scenarios use.
const (
	mccRestaurant = "5812"
	mccSoftware   = "5734"
	mccAirline    = "4511"
	mccTaxi       = "4121"
	mccMisc       = "5999"
	mccGambling   = "7995"
	mccHotel      = "7011"
)

// cardResult is how the engine decided one card's traffic.
type cardResult struct {
	requests, approved, declined, flagged int
	caught, passed                        bool
	firstFlagged, firstDeclined           int // attempt number, 0 when it never happened
	errors, fallbacks                     int
	latencyMS                             []float64 // the engine's own processing time per answer
}

// Scenario is a labeled kind of traffic: what is sent, and what the engine should do.
type Scenario struct {
	Name     string
	Expected string
	Fraud    bool // fraud is "caught"; normal traffic counts false declines
	Check    bool // a correctness check (pass / fail) rather than detection
	Cards    int
	play     func(ctx context.Context, run *Run, cardID string) cardResult
}

// ListScenarios returns the six scenarios of README §15, in report order.
func ListScenarios(normalCards, fraudCards int) []Scenario {
	return []Scenario{
		{Name: "normal traffic", Expected: "approve", Cards: normalCards, play: playNormal},
		{Name: "card testing", Expected: "flag, then decline", Fraud: true, Cards: fraudCards, play: playCardTesting},
		{Name: "split purchase", Expected: "flag", Fraud: true, Cards: fraudCards, play: playSplit},
		{Name: "stolen card", Expected: expectDecline, Fraud: true, Cards: fraudCards, play: playStolen},
		{Name: "gambling MCC", Expected: expectDecline, Fraud: true, Cards: fraudCards, play: playGambling},
		{Name: "hotel increment", Expected: "approve, hold raised", Check: true, Cards: fraudCards, play: playHotel},
	}
}

// Run is one simulator run: its random source, its engine and a counter for unique IDs.
type Run struct {
	ID     string
	Seed   uint64
	engine Engine
	random *rand.Rand
	seq    int
}

// NewRun returns a run whose traffic depends only on seed; id keeps its IDs unique.
func NewRun(id string, seed uint64, engine Engine) *Run {
	return &Run{ID: id, Seed: seed, engine: engine, random: rand.New(rand.NewPCG(seed, seed))}
}

// purchase is one authorization a scenario sends.
type purchase struct {
	merchantID, mcc, country, entryMode string
	amountMinor                         int64
}

func (r *Run) nextID(kind string) string {
	r.seq++
	return fmt.Sprintf("%s-%s-%d", r.ID, kind, r.seq)
}

// vary returns base plus a random amount in [-spread, spread], with odd cents.
func (r *Run) vary(base, spread int64) int64 {
	if spread == 0 {
		return base + oddCents
	}

	return base - spread + r.random.Int64N(2*spread+1) + oddCents
}

func (r *Run) authorize(ctx context.Context, cardID string, item purchase) (domain.AuthResponse, error) {
	return r.engine.Authorize(ctx, domain.AuthRequest{
		AuthorizationID: r.nextID("auth"),
		CardID:          cardID,
		AmountMinor:     item.amountMinor,
		Currency:        currency,
		Merchant:        domain.Merchant{ID: item.merchantID, Name: item.merchantID, MCC: item.mcc, Country: item.country},
		EntryMode:       item.entryMode,
		CreatedAt:       time.Now().UTC(),
	})
}

// send authorizes item and records the answer as attempt number attempt.
func (r *Run) send(ctx context.Context, cardID string, item purchase, attempt int, result *cardResult) domain.AuthResponse {
	response, err := r.authorize(ctx, cardID, item)

	result.requests++

	if err == nil {
		result.latencyMS = append(result.latencyMS, response.LatencyMS)
	}

	if response.FallbackUsed {
		result.fallbacks++
	}

	switch {
	case err != nil:
		result.errors++
	case response.Decision == domain.Decline:
		result.declined++
		if result.firstDeclined == 0 {
			result.firstDeclined = attempt
		}
	default:
		result.approved++
	}

	if response.Flagged {
		result.flagged++
		if result.firstFlagged == 0 {
			result.firstFlagged = attempt
		}
	}

	return response
}

// Kinds of everyday purchase.
const (
	kindLunch = iota
	kindSoftware
	kindAirline
	kindTaxi
	kindCount
)

// pickNormal is an everyday purchase: lunch, software, a flight or a taxi.
func (r *Run) pickNormal(cardID string) purchase {
	switch r.random.IntN(kindCount) {
	case kindLunch:
		return purchase{"m_lunch_" + cardID[len(cardID)-1:], mccRestaurant, countryHome, entryTap, r.vary(lunchMinor, lunchSpread)}
	case kindSoftware:
		return purchase{merchantSaaS, mccSoftware, countryHome, entryOnline, r.vary(softwareMinor, 0)}
	case kindAirline:
		return purchase{"m_airline", mccAirline, countryHome, entryOnline, r.vary(airlineMinor, airlineSpread)}
	default:
		return purchase{"m_taxi", mccTaxi, countryHome, entryWallet, r.vary(taxiMinor, taxiSpread)}
	}
}

func playNormal(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	for attempt := 1; attempt <= normalRequests; attempt++ {
		run.send(ctx, cardID, run.pickNormal(cardID), attempt, &result)
	}

	return result
}

// playCardTesting: tiny online charges at different merchants, seconds apart.
func playCardTesting(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	for attempt := 1; attempt <= cardTestingAttempts; attempt++ {
		amount := cardTestingMinMinor + run.random.Int64N(cardTestingSpread)
		item := purchase{fmt.Sprintf("m_shop_%d", attempt), mccMisc, countryHome, entryOnline, amount}
		run.send(ctx, cardID, item, attempt, &result)
	}

	result.caught = result.firstDeclined > 0

	return result
}

// playSplit: one purchase split in three parts just under the 500.00 limit.
func playSplit(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	for attempt := 1; attempt <= splitParts; attempt++ {
		amount := splitMinor + run.random.Int64N(splitSpread)
		run.send(ctx, cardID, purchase{"m_supplier", mccSoftware, countryHome, entryChip, amount}, attempt, &result)
	}

	result.caught = result.flagged > 0 || result.declined > 0

	return result
}

// playStolen: normal history at home, then a large online purchase abroad at
// a merchant this cardholder never used.
func playStolen(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	for attempt := 1; attempt <= stolenHistory; attempt++ {
		amount := run.vary(stolenUsualMinor, stolenSpread)
		run.send(ctx, cardID, purchase{merchantSaaS, mccSoftware, countryHome, entryChip, amount}, attempt, &result)
	}

	attack := purchase{"m_unknown_" + cardID, mccSoftware, countryAbroad, entryOnline, stolenMinor + run.random.Int64N(stolenAttackPad)}
	response := run.send(ctx, cardID, attack, stolenHistory+1, &result)
	result.caught = response.Decision == domain.Decline

	return result
}

func playGambling(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	response := run.send(ctx, cardID, purchase{"m_casino", mccGambling, countryHome, entryOnline, gamblingMinor + oddCents}, 1, &result)
	result.caught = response.Decision == domain.Decline

	return result
}

// playHotel: a check-in, then one more night as an incremental authorization.
func playHotel(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	auth := run.send(ctx, cardID, purchase{"m_hotel", mccHotel, countryHome, entryChip, hotelMinor + oddCents}, 1, &result)
	if auth.Decision != domain.Approve {
		return result
	}

	extra := int64(hotelExtraMinor)
	increment, err := run.engine.ApplyEvent(ctx, auth.AuthorizationID, pathIncrement, EventBody{EventID: run.nextID("event"), AmountMinor: &extra})

	result.requests++

	if err != nil {
		result.errors++
		return result
	}

	result.passed = increment.Decision == domain.Approve && increment.Status == lifecycle.StatusPending &&
		increment.HeldMinor == hotelMinor+oddCents+hotelExtraMinor

	return result
}
