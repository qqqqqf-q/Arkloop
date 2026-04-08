//go:build desktop

package sqlitepgx

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// typeCastRe matches PostgreSQL scalar/array casts like ::jsonb, ::text, ::uuid[] etc.
var typeCastRe = regexp.MustCompile(`::(?:jsonb|json|text|integer|bigint|boolean|uuid|inet)(?:\[\])?`)

// platformSkillConflictRe matches the PostgreSQL partial-index ON CONFLICT
// target used for platform skills: ON CONFLICT (account_id, skill_key, version)
// WHERE account_id IS NULL.  SQLite represents this with a partial unique index
// (skill_key, version) WHERE account_id IS NULL, so we rewrite the target.
var platformSkillConflictRe = regexp.MustCompile(
	`(?i)ON CONFLICT\s*\(\s*account_id\s*,\s*skill_key\s*,\s*version\s*\)\s*WHERE\s+account_id\s+IS\s+NULL`)

// intervalRe matches PostgreSQL interval literals like interval '30 days'.
var intervalRe = regexp.MustCompile(`(?i)interval\s+'(\d+)\s+(day|hour|minute|second)s?'`)

// datetimeNowAddRe matches datetime('now') + 'modifier' produced after interval rewriting,
// and rewrites it to the correct SQLite form datetime('now', 'modifier').
var datetimeNowAddRe = regexp.MustCompile(`datetime\('now'\)\s*\+\s*('[^']*')`)

// forUpdateRe strips PostgreSQL row-level locking clauses.
var forUpdateRe = regexp.MustCompile(`(?i)\s+FOR\s+(UPDATE|SHARE|NO\s+KEY\s+UPDATE|KEY\s+SHARE)(\s+SKIP\s+LOCKED|\s+NOWAIT)?`)

// jsonbSetCreateMissingTrueRe rewrites PostgreSQL jsonb_set(target, path, value, true)
// to SQLite json_set(target, path, value). The value parameter is then wrapped with
// json() by jsonSetValueParamRe to ensure JSON-encoded strings are stored as the
// correct JSON type rather than as SQL text.
var jsonbSetCreateMissingTrueRe = regexp.MustCompile(`(?i)jsonb_set\((.*?),\s*(.*?),\s*(.*?),\s*true\s*\)`)

// jsonSetValueParamRe wraps bare $N placeholders that appear as the third argument
// of json_set() with json(), so JSON-encoded values (e.g. "true", "[1,2,3]") are
// stored as their correct JSON types rather than as SQL text strings.
// Matches the tail fragment: '$.key', $N) which is unambiguous in our usage.
var jsonSetValueParamRe = regexp.MustCompile(`('\$\.[A-Za-z0-9_]+'\s*,\s*)(\$\d+)(\s*\))`)

// pgJSONPathRe rewrites simple PostgreSQL json path literals like '{foo}'
// to the SQLite form '$.foo'.
var pgJSONPathRe = regexp.MustCompile(`'\{([A-Za-z0-9_]+)\}'`)

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

	if strings.Contains(sql, "jsonb_set(") {
		sql = jsonbSetCreateMissingTrueRe.ReplaceAllString(sql, "json_set($1, $2, $3)")
		sql = strings.ReplaceAll(sql, "jsonb_set(", "json_set(")
	}

	if strings.Contains(sql, "json_set(") {
		sql = pgJSONPathRe.ReplaceAllString(sql, "'$$.$1'")
		// Wrap bare $N value params with json() so JSON-encoded strings
		// (e.g. "true", "false") are stored as proper JSON types, not SQL text.
		sql = jsonSetValueParamRe.ReplaceAllString(sql, "${1}json($2)${3}")
	}

	if intervalRe.MatchString(sql) {
		sql = intervalRe.ReplaceAllStringFunc(sql, rewriteInterval)
		// datetime('now') + '+N units' is not valid SQLite; rewrite to datetime('now', '+N units').
		if datetimeNowAddRe.MatchString(sql) {
			sql = datetimeNowAddRe.ReplaceAllString(sql, "datetime('now', ${1})")
		}
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

	// Rewrite platform-skill partial-index conflict target to match the SQLite
	// partial unique index (skill_key, version) WHERE account_id IS NULL.
	if platformSkillConflictRe.MatchString(sql) {
		sql = platformSkillConflictRe.ReplaceAllString(sql,
			"ON CONFLICT (skill_key, version) WHERE account_id IS NULL")
	}

	return sql
}

// rewriteInterval converts "interval '30 days'" to "'+30 days'" for use as a
// SQLite datetime modifier. The caller must also rewrite "datetime('now') + '+30 days'"
// to "datetime('now', '+30 days')" via datetimeNowAddRe.
func rewriteInterval(match string) string {
	parts := intervalRe.FindStringSubmatch(match)
	if len(parts) != 3 {
		return match
	}
	unit := strings.ToLower(parts[2])
	if !strings.HasSuffix(unit, "s") {
		unit += "s"
	}
	return "'+" + parts[1] + " " + unit + "'"
}

// anyParamRe matches "= ANY($N)" or "= ANY($N::type)" where $N is a parameter.
var anyParamRe = regexp.MustCompile(`=\s*ANY\(\s*\$(\d+)(?:::[^)]+)?\s*\)`)

// renumberParamRe matches bare "$N" placeholders.
var renumberParamRe = regexp.MustCompile(`\$(\d+)`)

// expandAnyArgs rewrites PostgreSQL "col = ANY($N)" to "col IN ($N, $N+1, ...)"
// by expanding string-compatible slice arguments, adjusting all subsequent $M indices accordingly.
func expandAnyArgs(sql string, args []any) (string, []any) {
	if !strings.Contains(sql, "ANY(") {
		return sql, args
	}
	matches := anyParamRe.FindAllStringSubmatchIndex(sql, -1)
	if len(matches) == 0 {
		return sql, args
	}

	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		paramIdx, err := strconv.Atoi(sql[m[2]:m[3]])
		if err != nil || paramIdx < 1 || paramIdx > len(args) {
			continue
		}
		slice := toStringSlice(args[paramIdx-1])
		if len(slice) == 0 {
			continue
		}

		placeholders := make([]string, len(slice))
		for j := range placeholders {
			placeholders[j] = fmt.Sprintf("$%d", paramIdx+j)
		}
		inClause := "IN (" + strings.Join(placeholders, ", ") + ")"

		suffix := sql[m[1]:]
		if len(slice) > 1 {
			suffix = renumberParamsFrom(suffix, paramIdx+1, len(slice)-1)
		}
		sql = sql[:m[0]] + inClause + suffix

		expanded := make([]any, len(slice))
		for j, s := range slice {
			expanded[j] = s
		}
		newArgs := make([]any, 0, len(args)-1+len(slice))
		newArgs = append(newArgs, args[:paramIdx-1]...)
		newArgs = append(newArgs, expanded...)
		newArgs = append(newArgs, args[paramIdx:]...)
		args = newArgs
	}
	return sql, args
}

// toStringSlice extracts a string-compatible slice from v, or returns nil if not applicable.
func toStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return append([]string(nil), s...)
	case []uuid.UUID:
		out := make([]string, len(s))
		for i := range s {
			out[i] = s[i].String()
		}
		return out
	}
	return nil
}

// renumberParamsFrom increments all $N with N >= startN in sql by delta.
// Only the portion of SQL outside string literals is safe to process here;
// in practice this is called on the tail suffix after an IN clause.
func renumberParamsFrom(sql string, startN, delta int) string {
	if delta == 0 {
		return sql
	}
	return renumberParamRe.ReplaceAllStringFunc(sql, func(m string) string {
		sub := renumberParamRe.FindStringSubmatch(m)
		n, err := strconv.Atoi(sub[1])
		if err != nil || n < startN {
			return m
		}
		return fmt.Sprintf("$%d", n+delta)
	})
}

// lateralRe detects LEFT JOIN LATERAL or JOIN LATERAL.
var lateralRe = regexp.MustCompile(`(?is)LEFT\s+JOIN\s+LATERAL|JOIN\s+LATERAL`)

// greatestRe matches PostgreSQL GREATEST() which is MAX() in SQLite.
var greatestRe = regexp.MustCompile(`(?i)GREATEST\(([^)]+)\)`)

var writeKeywordRe = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|REPLACE)\b`)

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

func queryRequiresWriteGuard(sql string) bool {
	trimmed := trimLeadingSQLComments(sql)
	if trimmed == "" {
		return false
	}
	fields := strings.Fields(strings.ToUpper(trimmed))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "INSERT", "UPDATE", "DELETE", "REPLACE":
		return true
	case "WITH":
		upper := strings.ToUpper(trimmed)
		return writeKeywordRe.MatchString(upper)
	default:
		return false
	}
}

func trimLeadingSQLComments(sql string) string {
	trimmed := strings.TrimSpace(sql)
	for trimmed != "" {
		switch {
		case strings.HasPrefix(trimmed, "--"):
			idx := strings.Index(trimmed, "\n")
			if idx < 0 {
				return ""
			}
			trimmed = strings.TrimSpace(trimmed[idx+1:])
		case strings.HasPrefix(trimmed, "/*"):
			idx := strings.Index(trimmed, "*/")
			if idx < 0 {
				return ""
			}
			trimmed = strings.TrimSpace(trimmed[idx+2:])
		default:
			return trimmed
		}
	}
	return ""
}
