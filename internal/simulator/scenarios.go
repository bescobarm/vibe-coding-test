package simulator

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
)

// Scenario parameters (README §15). Each run draws its cases from these
// ranges with its seed (D-066), so runs differ while every case keeps its
// expected outcome.
const (
	currency      = "USD"
	countryHome   = "US"
	entryChip     = "chip"
	entryWallet   = "wallet"
	entryOnline   = "ecommerce"
	entryTap      = "contactless"
	pathIncrement = "increment"
	expectDecline = "decline"
	merchantSaaS  = "m_saas"
	oddCents      = 37 // keeps amounts off round hundreds, so ROUND_AMOUNT stays quiet on normal traffic

	normalMinRequests, normalMaxRequests = 6, 10
	lunchPlaces, taxiCompanies           = 5, 3

	lunchMinor, lunchSpread     = 1500, 400
	softwareMinor               = 4900
	airlineMinor, airlineSpread = 32000, 4000
	taxiMinor, taxiSpread       = 2200, 500

	// Card testing: the rule declines from the 6th small charge, so a case has 6 to 10.
	cardTestingMinAttempts, cardTestingMaxAttempts = 6, 10
	cardTestingMinMinor, cardTestingMaxMinor       = 100, 499
	cardTestingMerchants                           = 1000 // testers hop between many merchants

	// Split: 2 to 4 parts, each above 80% of the 500.00 limit and under it.
	splitMinParts, splitMaxParts = 2, 4
	splitMinMinor, splitMaxMinor = 41000, 49900

	// Stolen card: 5 to 8 normal purchases (D-056 needs 5), then one abroad.
	stolenMinHistory, stolenMaxHistory = 5, 8
	stolenUsualMinor, stolenSpread     = 4500, 300
	stolenMinMinor, stolenMaxMinor     = 20000, 45000

	blockedMinMinor, blockedMaxMinor = 2000, 9000

	// Hotel: a room plus one more night, under the 500.00 per-transaction limit.
	hotelMinMinor, hotelMaxMinor = 20000, 30000
	extraMinMinor, extraMaxMinor = 5000, 15000
)

// Countries a stolen card is used in, and the categories the policy blocks.
var (
	countriesAbroad = []string{
		"BR",
		"NG",
		"GB",
		"CN",
		"RO",
	}
	blockedMCCs = []string{
		mccGambling,
		mccQuasiCash,
		mccMoneyTransfer,
	}
)

// Merchants and MCCs the scenarios use.
const (
	mccRestaurant    = "5812"
	mccSoftware      = "5734"
	mccAirline       = "4511"
	mccTaxi          = "4121"
	mccMisc          = "5999"
	mccGambling      = "7995"
	mccQuasiCash     = "6051"
	mccMoneyTransfer = "4829"
	mccHotel         = "7011"
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
		{Name: "blocked MCC", Expected: expectDecline, Fraud: true, Cards: fraudCards, play: playBlocked},
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

// between returns a random number in [low, high].
func (r *Run) between(low, high int64) int64 {
	return low + r.random.Int64N(high-low+1)
}

// pickFrom returns a random item of list.
func (r *Run) pickFrom(list []string) string {
	return list[r.random.IntN(len(list))]
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
		return purchase{fmt.Sprintf("m_lunch_%d", r.random.IntN(lunchPlaces)), mccRestaurant, countryHome, entryTap, r.vary(lunchMinor, lunchSpread)}
	case kindSoftware:
		return purchase{merchantSaaS, mccSoftware, countryHome, entryOnline, r.vary(softwareMinor, 0)}
	case kindAirline:
		return purchase{"m_airline", mccAirline, countryHome, entryOnline, r.vary(airlineMinor, airlineSpread)}
	default:
		return purchase{fmt.Sprintf("m_taxi_%d", r.random.IntN(taxiCompanies)), mccTaxi, countryHome, entryWallet, r.vary(taxiMinor, taxiSpread)}
	}
}

func playNormal(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	requests := int(run.between(normalMinRequests, normalMaxRequests))

	for attempt := 1; attempt <= requests; attempt++ {
		run.send(ctx, cardID, run.pickNormal(cardID), attempt, &result)
	}

	return result
}

// playCardTesting: tiny online charges at different merchants, seconds apart.
func playCardTesting(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	attempts := int(run.between(cardTestingMinAttempts, cardTestingMaxAttempts))

	for attempt := 1; attempt <= attempts; attempt++ {
		amount := run.between(cardTestingMinMinor, cardTestingMaxMinor)
		item := purchase{fmt.Sprintf("m_shop_%d", run.random.IntN(cardTestingMerchants)), mccMisc, countryHome, entryOnline, amount}
		run.send(ctx, cardID, item, attempt, &result)
	}

	result.caught = result.firstDeclined > 0

	return result
}

// playSplit: one purchase split in three parts just under the 500.00 limit.
func playSplit(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	parts := int(run.between(splitMinParts, splitMaxParts))

	for attempt := 1; attempt <= parts; attempt++ {
		amount := run.between(splitMinMinor, splitMaxMinor)
		run.send(ctx, cardID, purchase{"m_supplier", mccSoftware, countryHome, entryChip, amount}, attempt, &result)
	}

	result.caught = result.flagged > 0 || result.declined > 0

	return result
}

// playStolen: normal history at home, then a large online purchase abroad at
// a merchant this cardholder never used.
func playStolen(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	history := int(run.between(stolenMinHistory, stolenMaxHistory))

	for attempt := 1; attempt <= history; attempt++ {
		amount := run.vary(stolenUsualMinor, stolenSpread)
		run.send(ctx, cardID, purchase{merchantSaaS, mccSoftware, countryHome, entryChip, amount}, attempt, &result)
	}

	abroad := run.pickFrom(countriesAbroad)
	attack := purchase{"m_unknown_" + cardID, mccSoftware, abroad, entryOnline, run.between(stolenMinMinor, stolenMaxMinor)}
	response := run.send(ctx, cardID, attack, history+1, &result)
	result.caught = response.Decision == domain.Decline

	return result
}

func playBlocked(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	blocked := purchase{"m_blocked", run.pickFrom(blockedMCCs), countryHome, entryOnline, run.between(blockedMinMinor, blockedMaxMinor)}
	response := run.send(ctx, cardID, blocked, 1, &result)
	result.caught = response.Decision == domain.Decline

	return result
}

// playHotel: a check-in, then one more night as an incremental authorization.
func playHotel(ctx context.Context, run *Run, cardID string) cardResult {
	var result cardResult

	room := run.between(hotelMinMinor, hotelMaxMinor)

	auth := run.send(ctx, cardID, purchase{"m_hotel", mccHotel, countryHome, entryChip, room}, 1, &result)
	if auth.Decision != domain.Approve {
		return result
	}

	extra := run.between(extraMinMinor, extraMaxMinor)
	increment, err := run.engine.ApplyEvent(ctx, auth.AuthorizationID, pathIncrement, EventBody{EventID: run.nextID("event"), AmountMinor: &extra})

	result.requests++

	if err != nil {
		result.errors++
		return result
	}

	result.passed = increment.Decision == domain.Approve && increment.Status == lifecycle.StatusPending &&
		increment.HeldMinor == room+extra

	return result
}
