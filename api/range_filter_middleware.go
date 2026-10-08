package api

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// rangeValueKind says how a range bound's query value is parsed.
type rangeValueKind int

const (
	rangeKindTime   rangeValueKind = iota // RFC 3339 timestamp
	rangeKindNumber                       // finite float
)

// rangePair names the two query parameters that bound one range filter and
// whether each bound is exclusive for the operation that serves it. A range is
// rejected iff it is empty: lower > upper, or lower == upper when either bound
// is exclusive.
type rangePair struct {
	Lower          string
	Upper          string
	Kind           rangeValueKind
	LowerExclusive bool
	UpperExclusive bool
}

// timePair builds a timestamp range pair.
// SEM@fa62a7fedb2dc47713f3862a6c20ac96e2b1bc8e: build a timestamp range pair from lower and upper parameter names (pure)
func timePair(lower, upper string, lowerExclusive, upperExclusive bool) rangePair {
	return rangePair{Lower: lower, Upper: upper, Kind: rangeKindTime, LowerExclusive: lowerExclusive, UpperExclusive: upperExclusive}
}

// inclusiveTimePairs builds `<name>_after` / `<name>_before` timestamp pairs
// whose bounds are both inclusive (>= / <=), the semantics of every store
// except the threat and usability-feedback lists.
// SEM@fa62a7fedb2dc47713f3862a6c20ac96e2b1bc8e: build inclusive after/before timestamp range pairs for named fields (pure)
func inclusiveTimePairs(names ...string) []rangePair {
	pairs := make([]rangePair, 0, len(names))
	for _, n := range names {
		pairs = append(pairs, timePair(n+"_after", n+"_before", false, false))
	}
	return pairs
}

// rangeFilterRoutes maps a gin route template (c.FullPath()) to the range
// filters its GET operation accepts. Add a row here when an operation gains a
// lower/upper query pair, and document the constraint in the OpenAPI spec.
//
// Exclusivity mirrors the store behind each operation:
//   - threats list: created_at and modified_at use > and <; score_gt/score_lt
//     are exclusive and score_ge/score_le inclusive (threat_store_gorm.go)
//   - usability_feedback: created_at >= after, < before (usability_feedback_store_gorm.go)
//   - everything else: >= and <= (inclusive)
//
// The survey list operations declare the parameters but do not filter on them
// today; they use the inclusive default.
var rangeFilterRoutes = map[string][]rangePair{
	"/threat_models/:threat_model_id/threats": {
		timePair("created_after", "created_before", true, true),
		timePair("modified_after", "modified_before", true, true),
		{Lower: "score_gt", Upper: "score_lt", Kind: rangeKindNumber, LowerExclusive: true, UpperExclusive: true},
		{Lower: "score_gt", Upper: "score_le", Kind: rangeKindNumber, LowerExclusive: true},
		{Lower: "score_ge", Upper: "score_lt", Kind: rangeKindNumber, UpperExclusive: true},
		{Lower: "score_ge", Upper: "score_le", Kind: rangeKindNumber},
	},
	"/usability_feedback":                         {timePair("created_after", "created_before", false, true)},
	"/threat_models":                              inclusiveTimePairs("created", "modified", "status_updated"),
	"/threat_models/:threat_model_id/audit_trail": inclusiveTimePairs("created"),
	"/admin/audit/system":                         inclusiveTimePairs("created"),
	"/admin/audit/threat_models":                  inclusiveTimePairs("created"),
	"/admin/users":                                inclusiveTimePairs("created", "last_login"),
	"/admin/surveys":                              inclusiveTimePairs("created", "modified"),
	"/intake/surveys":                             inclusiveTimePairs("created", "modified"),
	"/intake/survey_responses":                    inclusiveTimePairs("created", "modified"),
	"/triage/survey_responses":                    inclusiveTimePairs("created", "modified"),
}

// compareRangeBounds compares the raw lower and upper bound values (-1, 0, +1).
// It reports false when either value is unparsable, so format errors are left
// to the existing validation.
// SEM@fa62a7fedb2dc47713f3862a6c20ac96e2b1bc8e: compare two raw range bound strings as instants or finite numbers (pure)
func compareRangeBounds(kind rangeValueKind, lowRaw, upRaw string) (int, bool) {
	switch kind {
	case rangeKindTime:
		low, err1 := time.Parse(time.RFC3339, lowRaw)
		up, err2 := time.Parse(time.RFC3339, upRaw)
		if err1 != nil || err2 != nil {
			return 0, false
		}
		return low.Compare(up), true
	case rangeKindNumber:
		// Scores bind as float32 in the generated API, so compare at that precision.
		lowF, err1 := strconv.ParseFloat(lowRaw, 32)
		upF, err2 := strconv.ParseFloat(upRaw, 32)
		if err1 != nil || err2 != nil || math.IsNaN(lowF) || math.IsNaN(upF) || math.IsInf(lowF, 0) || math.IsInf(upF, 0) {
			return 0, false
		}
		low, up := float32(lowF), float32(upF)
		switch {
		case low < up:
			return -1, true
		case low > up:
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

// rangeIsEmpty reports whether the request's bounds for the pair describe an
// empty range. Missing, empty, or unparsable bounds never do.
// SEM@fa62a7fedb2dc47713f3862a6c20ac96e2b1bc8e: detect an empty range among a pair's query bounds (pure)
func rangeIsEmpty(c *gin.Context, p rangePair) bool {
	lowRaw, okLow := c.GetQuery(p.Lower)
	upRaw, okUp := c.GetQuery(p.Upper)
	if !okLow || !okUp || lowRaw == "" || upRaw == "" {
		return false
	}
	cmp, ok := compareRangeBounds(p.Kind, lowRaw, upRaw)
	if !ok {
		return false
	}
	return cmp > 0 || (cmp == 0 && (p.LowerExclusive || p.UpperExclusive))
}

// RangeFilterValidationMiddleware rejects GET list requests whose range filters
// describe an empty range (lower bound above upper bound, or equal when either
// bound is exclusive) with 400 invalid_input. It is the single enforcement
// point for all range query pairs (#1051).
// SEM@fa62a7fedb2dc47713f3862a6c20ac96e2b1bc8e: reject list requests whose range query bounds form an empty range with 400 (pure)
func RangeFilterValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			c.Next()
			return
		}
		for _, p := range rangeFilterRoutes[c.FullPath()] {
			if rangeIsEmpty(c, p) {
				relation := "must not be greater than"
				if p.LowerExclusive || p.UpperExclusive {
					relation = "must be less than"
				}
				HandleRequestError(c, InvalidInputError(fmt.Sprintf(
					"%s %s %s: the requested range is empty", p.Lower, relation, p.Upper)))
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
