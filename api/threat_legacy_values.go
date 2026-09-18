package api

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"
)

// legacyThreatValues maps legacy stored values of the threat severity,
// priority and status columns to their canonical form (#925). Keys are
// lowercase. The numeric keys and the display strings are what older tmi-ux
// builds wrote (tm-edit-formatting.service.ts severityMap/priorityMap/
// statusMap); note the numeric severity keys run opposite to the sort rank:
// "0" was critical. "none" -> "informational" predates #925 (startup
// migration in cmd/server). The columns stay free-form: anything not listed
// here or in the rank tables is left untouched.
var legacyThreatValues = map[string]map[string]string{
	"severity": {
		"0": "critical", "1": "high", "2": "medium", "3": "low", "4": "informational", "5": "unknown",
		"info": "informational", "none": "informational",
	},
	"priority": {
		"0": "immediate", "1": "high", "2": "medium", "3": "low", "4": "deferred",
		"immediate (p0)": "immediate", "high (p1)": "high", "medium (p2)": "medium",
		"low (p3)": "low", "deferred (p4)": "deferred",
	},
	"status": {
		"0": "open", "1": "confirmed", "2": "mitigation_planned", "3": "mitigation_in_progress",
		"4": "verification_pending", "5": "resolved", "6": "accepted", "7": "false_positive",
		"8": "deferred", "9": "closed",
		"mitigation planned": "mitigation_planned", "mitigation in progress": "mitigation_in_progress",
		"verification pending": "verification_pending", "false positive": "false_positive",
	},
}

// canonicalThreatValue returns the canonical form of a severity, priority or
// status value: a legacy value is mapped, a known value in the wrong case is
// lowercased, and anything else (free-form) is returned unchanged.
// SEM@d4baf9204f11e11bdb71462a0ea5af2d70f2cad5: convert a legacy or mis-cased threat field value to its canonical form (pure)
func canonicalThreatValue(column, value string) string {
	legacy, ok := legacyThreatValues[column]
	if !ok {
		return value
	}
	lower := strings.ToLower(value)
	if canonical, ok := legacy[lower]; ok {
		return canonical
	}
	if _, ok := semanticOrderMaps[column][lower]; ok {
		return lower
	}
	return value
}

// canonicalizeThreatValues rewrites the threat's severity, priority and status
// in place so legacy values cannot be stored again (#925).
// SEM@d4baf9204f11e11bdb71462a0ea5af2d70f2cad5: normalize a threat's severity, priority, and status to canonical values (mutates input)
func canonicalizeThreatValues(threat *Threat) {
	for column, field := range map[string]**string{
		"severity": &threat.Severity, "priority": &threat.Priority, "status": &threat.Status,
	} {
		if *field != nil {
			v := canonicalThreatValue(column, **field)
			*field = &v
		}
	}
}

// canonicalThreatValues maps canonicalThreatValue over a filter value list.
// SEM@d4baf9204f11e11bdb71462a0ea5af2d70f2cad5: convert a list of threat filter values to canonical form (pure)
func canonicalThreatValues(column string, values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = canonicalThreatValue(column, v)
	}
	return out
}

// MigrateLegacyThreatValues rewrites stored legacy severity, priority and
// status values to canonical form and returns the number of values changed.
// Idempotent: keys and values of each map are disjoint, so a second run
// matches nothing. Keys are emitted in sorted order so the SQL text is stable.
//
// Runs in one explicit READ COMMITTED transaction: a bare Exec would inherit
// whatever isolation the pooled Oracle session last had, and a full-scan
// UPDATE under a leftover SERIALIZABLE raises ORA-08177 instead of restarting
// (#906). The LOWER() predicates cannot use the column indexes; the caller is
// expected to drop this once every environment has been migrated (#926).
// SEM@d4baf9204f11e11bdb71462a0ea5af2d70f2cad5: rewrite stored legacy threat severity, priority, and status values to canonical form (writes DB)
func MigrateLegacyThreatValues(ctx context.Context, db *gorm.DB) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, column := range []string{"severity", "priority", "status"} {
			legacy := legacyThreatValues[column]
			keys := make([]string, 0, len(legacy))
			for k := range legacy {
				keys = append(keys, k)
			}
			slices.Sort(keys)

			// column comes from the fixed list above, never from input.
			var b strings.Builder
			fmt.Fprintf(&b, "UPDATE threats SET %s = CASE LOWER(%s)", column, column)
			args := make([]any, 0, 2*len(keys)+1)
			for _, k := range keys {
				b.WriteString(" WHEN ? THEN ?")
				args = append(args, k, legacy[k])
			}
			fmt.Fprintf(&b, " END WHERE LOWER(%s) IN ?", column)
			args = append(args, keys)
			res := tx.Exec(b.String(), args...)
			if res.Error != nil {
				return fmt.Errorf("migrate legacy threat %s values: %w", column, res.Error)
			}
			total += res.RowsAffected

			if column == "severity" {
				// cmd/server already lowercases every severity just before this.
				continue
			}
			known := make([]string, 0, len(semanticOrderMaps[column]))
			for k := range semanticOrderMaps[column] {
				known = append(known, k)
			}
			slices.Sort(known)
			res = tx.Exec(
				fmt.Sprintf("UPDATE threats SET %s = LOWER(%s) WHERE %s <> LOWER(%s) AND LOWER(%s) IN ?",
					column, column, column, column, column), known)
			if res.Error != nil {
				return fmt.Errorf("lowercase known threat %s values: %w", column, res.Error)
			}
			total += res.RowsAffected
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	return total, nil
}
