//go:build desktop

package sqlitepgx

import (
	"regexp"
	"strings"
)

// typeCastRe matches PostgreSQL type casts like ::jsonb, ::text, ::uuid etc.
var typeCastRe = regexp.MustCompile(`::(?:jsonb|json|text|integer|bigint|boolean|uuid)\b`)

// intervalRe matches PostgreSQL interval literals like interval '30 days'.
var intervalRe = regexp.MustCompile(`(?i)interval\s+'(\d+)\s+(day|hour|minute|second)s?'`)

// forUpdateRe strips PostgreSQL row-level locking clauses.
var forUpdateRe = regexp.MustCompile(`(?i)\s+FOR\s+(UPDATE|SHARE|NO\s+KEY\s+UPDATE|KEY\s+SHARE)(\s+SKIP\s+LOCKED|\s+NOWAIT)?`)

// rewriteSQL performs lightweight PostgreSQL-to-SQLite SQL preprocessing.
// Only applies transformations when PG-specific patterns are detected.
func rewriteSQL(sql string) string {
	if strings.Contains(sql, "now()") {
		sql = strings.ReplaceAll(sql, "now()", "datetime('now')")
	}

	if strings.Contains(sql, "ILIKE") {
		sql = strings.ReplaceAll(sql, "ILIKE", "LIKE")
	}
	if strings.Contains(sql, "ilike") {
		sql = strings.ReplaceAll(sql, "ilike", "LIKE")
	}

	if strings.Contains(sql, "::") {
		sql = typeCastRe.ReplaceAllString(sql, "")
	}

	if intervalRe.MatchString(sql) {
		sql = intervalRe.ReplaceAllStringFunc(sql, rewriteInterval)
	}

	if forUpdateRe.MatchString(sql) {
		sql = forUpdateRe.ReplaceAllString(sql, "")
	}

	return sql
}

// rewriteInterval converts "interval '30 days'" to "'+30 days'" for SQLite datetime().
func rewriteInterval(match string) string {
	parts := intervalRe.FindStringSubmatch(match)
	if len(parts) != 3 {
		return match
	}
	unit := strings.ToLower(parts[2])
	if !strings.HasSuffix(unit, "s") {
		unit += "s"
	}
	return "'+'" + parts[1] + " " + unit + "'"
}
