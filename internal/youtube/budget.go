package youtube

import (
	"context"
	"errors"
	"fmt"
	"time"

	// Embeds the timezone database so QuotaDay works in a scratch container,
	// where there is no system tzdata to load.
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
)

// SearchCallsPerDay is Google's hard per-project limit on search.list. It is
// not configurable because it is not ours to set.
const SearchCallsPerDay = 100

var quotaLocation = time.FixedZone("PT", -8*60*60)

func init() {
	// Google's quota resets at midnight Pacific, which observes DST. Fall back
	// to fixed PST only if the zone database is somehow unavailable.
	if loc, err := time.LoadLocation("America/Los_Angeles"); err == nil {
		quotaLocation = loc
	}
}

// QuotaDay is the ledger key for t, in the timezone Google resets quota on.
func QuotaDay(t time.Time) string {
	return t.In(quotaLocation).Format("2006-01-02")
}

// Budget caps daily spend for one family.
type Budget struct {
	// Units caps general endpoints. Google allows 10,000.
	Units int
	// Searches caps search.list, metered in its own bucket of 100.
	Searches int
	// Backfill caps back-catalog calls, which only spend what is left over.
	Backfill int
}

// Spend is consumption so far today for one purpose.
type Spend struct {
	Units int
	Calls int
}

// Ledger persists daily spend. It lives in Postgres rather than memory so a
// crash loop cannot re-spend the day's allocation on every restart.
type Ledger interface {
	Record(ctx context.Context, familyID uuid.UUID, day string,
		purpose domain.QuotaPurpose, units, calls int) error
	Usage(ctx context.Context, familyID uuid.UUID, day string) (map[domain.QuotaPurpose]Spend, error)
}

// ErrBudgetExhausted reports that a purpose has hit its ceiling for the day.
// Callers surface it as 429 and fall back to cached data.
var ErrBudgetExhausted = errors.New("daily API budget exhausted")

// BudgetError carries which ceiling was hit and when it clears.
type BudgetError struct {
	Purpose  domain.QuotaPurpose
	Used     int
	Limit    int
	ResetsAt time.Time
}

func (e *BudgetError) Error() string {
	return fmt.Sprintf("%s budget exhausted: %d of %d used, resets %s",
		e.Purpose, e.Used, e.Limit, e.ResetsAt.Format(time.RFC3339))
}

func (e *BudgetError) Unwrap() error { return ErrBudgetExhausted }

// NextQuotaReset is the next midnight in Google's quota timezone.
func NextQuotaReset(now time.Time) time.Time {
	local := now.In(quotaLocation)
	return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, quotaLocation)
}

// limitFor reports the ceiling for a purpose, and whether it is metered in
// units or in calls. Search and backfill are counted per call; the general
// endpoints are counted in units.
func (b Budget) limitFor(purpose domain.QuotaPurpose) (limit int, byCalls bool) {
	switch purpose {
	case domain.PurposeSearch:
		return min(b.Searches, SearchCallsPerDay), true
	case domain.PurposeBackfill:
		return b.Backfill, true
	default:
		return b.Units, false
	}
}

// check reports whether spending cost more against purpose stays in budget.
// Read then act, so concurrent callers can overshoot by one call: deliberate,
// since default ceilings sit under Google's and locking every call is worse.
func check(usage map[domain.QuotaPurpose]Spend, budget Budget,
	purpose domain.QuotaPurpose, cost int, now time.Time) error {

	limit, byCalls := budget.limitFor(purpose)
	if limit <= 0 {
		return &BudgetError{Purpose: purpose, Used: 0, Limit: limit, ResetsAt: NextQuotaReset(now)}
	}

	spent := usage[purpose]
	used := spent.Units
	if byCalls {
		used = spent.Calls
	}

	if used+cost > limit {
		return &BudgetError{
			Purpose:  purpose,
			Used:     used,
			Limit:    limit,
			ResetsAt: NextQuotaReset(now),
		}
	}
	return nil
}
