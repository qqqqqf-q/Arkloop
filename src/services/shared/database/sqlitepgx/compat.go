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

	if lateralRe.MatchString(sql) {
		sql = rewriteLateral(sql)
	}

	if strings.Contains(sql, "GREATEST(") || strings.Contains(sql, "greatest(") {
		sql = greatestRe.ReplaceAllString(sql, "MAX($1)")
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

// lateralRe detects LEFT JOIN LATERAL or JOIN LATERAL.
var lateralRe = regexp.MustCompile(`(?is)LEFT\s+JOIN\s+LATERAL|JOIN\s+LATERAL`)

// greatestRe matches PostgreSQL GREATEST() which is MAX() in SQLite.
var greatestRe = regexp.MustCompile(`(?i)GREATEST\(([^)]+)\)`)

// rewriteLateral converts LEFT JOIN LATERAL (subquery) alias ON true
// to correlated subqueries in the SELECT clause.
// Only handles single-column LATERAL subqueries with ON true.
func rewriteLateral(sql string) string {
	// Match: LEFT JOIN LATERAL ( ... ) alias ON true
	// Strategy: find each LATERAL block, extract subquery + alias,
	// remove the JOIN, replace alias.col references with the subquery.
	re := regexp.MustCompile(`(?is)(LEFT\s+)?JOIN\s+LATERAL\s*\(` +
		`((?:[^()]*|\((?:[^()]*|\([^()]*\))*\))*)` + // nested parens up to 2 levels
		`\)\s+(\w+)\s+ON\s+true`)

	for {
		loc := re.FindStringSubmatchIndex(sql)
		if loc == nil {
			break
		}
		fullStart, fullEnd := loc[0], loc[1]
		subquery := strings.TrimSpace(sql[loc[4]:loc[5]])
		alias := sql[loc[6]:loc[7]]

		// Remove the JOIN LATERAL clause
		sql = sql[:fullStart] + sql[fullEnd:]

		// Replace alias.column references with the subquery
		// Common pattern: alias.column_name (optionally followed by AS)
		colRef := regexp.MustCompile(`\b` + regexp.QuoteMeta(alias) + `\.(\w+)`)
		sql = colRef.ReplaceAllString(sql, "("+subquery+") /* $1 */")
	}
	return sql
}
