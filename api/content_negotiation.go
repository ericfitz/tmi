package api

import (
	"sort"
	"strconv"
	"strings"
)

// negotiateContentType selects the best response media type for a request's
// Accept header from the server's offered types, listed in server-preference
// order (offered[0] is the default).
//
// Behavior:
//   - An empty Accept header selects offered[0] (the default).
//   - Media ranges are honored with q-values; "*/*" and "type/*" wildcards match.
//   - Among equal q-values, the client's listed order is preserved, and the
//     first offered type that matches a client range wins.
//   - Returns (chosen, true) on a match, or ("", false) when no offered type is
//     acceptable (the caller should respond 406 Not Acceptable).
func negotiateContentType(acceptHeader string, offered []string) (string, bool) {
	if len(offered) == 0 {
		return "", false
	}
	accept := strings.TrimSpace(acceptHeader)
	if accept == "" {
		return offered[0], true
	}

	type mediaRange struct {
		typ string
		sub string
		q   float64
		idx int
	}
	var ranges []mediaRange
	for i, part := range strings.Split(accept, ",") {
		tok := strings.TrimSpace(part)
		if tok == "" {
			continue
		}
		segs := strings.Split(tok, ";")
		media := strings.ToLower(strings.TrimSpace(segs[0]))
		q := 1.0
		for _, p := range segs[1:] {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "q=") {
				if v, err := strconv.ParseFloat(strings.TrimPrefix(p, "q="), 64); err == nil {
					q = v
				}
			}
		}
		t, s, ok := strings.Cut(media, "/")
		if !ok {
			continue
		}
		ranges = append(ranges, mediaRange{typ: t, sub: s, q: q, idx: i})
	}

	// Highest q first; ties broken by the client's original order.
	sort.SliceStable(ranges, func(a, b int) bool {
		if ranges[a].q != ranges[b].q {
			return ranges[a].q > ranges[b].q
		}
		return ranges[a].idx < ranges[b].idx
	})

	for _, r := range ranges {
		if r.q <= 0 { // q=0 explicitly rejects a type
			continue
		}
		for _, off := range offered {
			ot, os, ok := strings.Cut(strings.ToLower(off), "/")
			if !ok {
				continue
			}
			if (r.typ == "*" || r.typ == ot) && (r.sub == "*" || r.sub == os) {
				return off, true
			}
		}
	}
	return "", false
}
