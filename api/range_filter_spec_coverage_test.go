package api

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// rangeFilterSpecExemptions lists spec range pairs that are intentionally not in
// rangeFilterRoutes. Keys are "<gin route> <lower>/<upper>".
var rangeFilterSpecExemptions = map[string]string{
	// The handler already rejects start_date > end_date (timmy_handlers.go).
	"/admin/timmy/usage start_date/end_date": "validated in handler",
}

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// specRangePairs finds every lower/upper query pair on GET operations by name
// convention, keyed "<gin route> <lower>/<upper>".
func specRangePairs(t *testing.T) map[string]bool {
	t.Helper()
	swagger, err := GetSwagger()
	require.NoError(t, err)
	found := map[string]bool{}
	for path, item := range swagger.Paths.Map() {
		if item.Get == nil {
			continue
		}
		names := map[string]bool{}
		for _, p := range item.Get.Parameters {
			if p != nil && p.Value != nil && p.Value.In == "query" {
				names[p.Value.Name] = true
			}
		}
		route := pathParamRe.ReplaceAllString(path, ":$1")
		add := func(lower, upper string) {
			if names[lower] && names[upper] {
				found[route+" "+lower+"/"+upper] = true
			}
		}
		for n := range names {
			switch {
			case strings.HasSuffix(n, "_after"):
				add(n, strings.TrimSuffix(n, "_after")+"_before")
			case strings.HasPrefix(n, "start_"):
				add(n, "end_"+strings.TrimPrefix(n, "start_"))
			case strings.HasSuffix(n, "_min"):
				add(n, strings.TrimSuffix(n, "_min")+"_max")
			}
		}
		for _, lo := range []string{"score_gt", "score_ge"} {
			for _, up := range []string{"score_lt", "score_le"} {
				add(lo, up)
			}
		}
	}
	return found
}

// TestRangeFilterTableMatchesSpec fails when a spec range pair is missing from
// rangeFilterRoutes (or exempted), and when a table entry has no spec pair.
func TestRangeFilterTableMatchesSpec(t *testing.T) {
	spec := specRangePairs(t)
	require.NotEmpty(t, spec, "no range pairs found in spec; detection is broken")

	table := map[string]bool{}
	for route, pairs := range rangeFilterRoutes {
		for _, p := range pairs {
			table[route+" "+p.Lower+"/"+p.Upper] = true
		}
	}

	var missing, stale []string
	for k := range spec {
		if !table[k] && rangeFilterSpecExemptions[k] == "" {
			missing = append(missing, k)
		}
	}
	for k := range table {
		if !spec[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	require.Empty(t, missing, "spec range pairs missing from rangeFilterRoutes (add them or exempt with a reason)")
	require.Empty(t, stale, "rangeFilterRoutes entries with no matching GET query pair in the spec")
	for k := range rangeFilterSpecExemptions {
		require.True(t, spec[k], "exemption %q no longer matches a spec pair", k)
	}
}
