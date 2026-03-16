//go:build !desktop

package pgadapter

import "arkloop/services/shared/database"

// Dialect returns a PostgresDialect instance.
func Dialect() database.DialectHelper {
	return database.PostgresDialect{}
}
