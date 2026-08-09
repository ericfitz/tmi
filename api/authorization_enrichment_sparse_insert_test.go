package api

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// internalUUIDEqualityPattern matches an equality filter on internal_uuid in
// either the Postgres dialector's rendering (quoted identifier, "$"-style
// bind, e.g. `internal_uuid" = $1`) or Oracle's (SkipQuoteIdentifiers means
// unquoted and uppercase, ":"-style bind, e.g. `INTERNAL_UUID = :4`).
var internalUUIDEqualityPattern = regexp.MustCompile(`(?i)internal_uuid"?\s*=\s*[$:]`)

// guardedQueryMatcher wraps sqlmock's regexp matcher with a hard failure if
// any query filters on internal_uuid by equality. That predicate only ever
// appears because GORM auto-adds "WHERE <pk> = <value>" when the destination
// struct's primary key is already populated — which is exactly what happens
// if a duplicate-key recovery re-fetch reuses the struct BeforeCreate already
// stamped a (never-inserted) UUID onto. Regression guard for that bug (#718).
// SEM@8b3ed9b61b0621b01f6928cc867b692933b7ad5b: build a sqlmock query matcher that rejects queries filtering by internal_uuid (test helper)
func guardedQueryMatcher(t *testing.T) sqlmock.QueryMatcher {
	t.Helper()
	return sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if internalUUIDEqualityPattern.MatchString(actualSQL) {
			return fmt.Errorf("query must not filter on internal_uuid by equality (leftover PK from a failed insert would silently miss on re-fetch): %s", actualSQL)
		}
		matched, err := regexp.MatchString(expectedSQL, actualSQL)
		if err != nil {
			return err
		}
		if !matched {
			return fmt.Errorf("actual sql: %q does not match expected regexp %q", actualSQL, expectedSQL)
		}
		return nil
	})
}

// newMockGormDB wires a sqlmock *sql.DB behind a Postgres GORM dialector, for
// unit tests that need to control exactly what error a query returns without
// a real database connection.
// SEM@8ea37221e3186b49d52e78d8834a4e6dd35d2b93: build a GORM DB backed by sqlmock for controlled-error unit tests (test helper)
func newMockGormDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(guardedQueryMatcher(t)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	gdb, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	require.NoError(t, err)

	return gdb, mock
}

// #718 — performSparseUserInsert must recover from a duplicate-key error on
// the sparse insert (a concurrent request won the race to create the same
// user row) by re-fetching the winning row instead of surfacing a 500.
// SEM@8ea37221e3186b49d52e78d8834a4e6dd35d2b93: validate sparse user insert recovers a provider-lookup duplicate-key race by refetching the winning row
func TestPerformSparseUserInsert_DuplicateKeyRefetchesWinner(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	mock.MatchExpectationsInOrder(true)

	email := openapi_types.Email("alice@example.com")
	authEntry := &Authorization{
		PrincipalType: AuthorizationPrincipalTypeUser,
		Provider:      "google",
		ProviderId:    "google-123",
		Email:         &email,
	}

	// FirstOrCreate's own lookup: no existing row (this request lost the race
	// to a concurrent insert that hasn't been visible to this SELECT yet).
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"internal_uuid"}))

	// The INSERT collides with idx_users_provider_lookup (unique on
	// provider + provider_user_id, #701) because the concurrent insert won.
	// BeforeCreate stamps a fresh InternalUUID onto the destination struct as
	// a side effect of this call even though the INSERT itself fails.
	mock.ExpectQuery(`INSERT INTO "users"`).
		WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint \"idx_users_provider_lookup\""})

	// Re-fetch after the classified duplicate: must query by (provider,
	// provider_user_id/email) only — NOT by the leftover internal_uuid from
	// the failed insert above (guardedQueryMatcher fails the test if it does)
	// — and returns the winner's row.
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"internal_uuid", "provider", "provider_user_id", "email", "name"}).
			AddRow("uuid-winner", "google", "google-123", "alice@example.com", "Alice"))

	err := performSparseUserInsert(context.Background(), gdb, authEntry)
	require.NoError(t, err, "duplicate-key create conflict must be recovered by re-fetching, not surfaced as an error")

	require.NoError(t, mock.ExpectationsWereMet())
}

// #720 — performSparseUserInsert must recover the same way from a duplicate
// hit on idx_users_sparse_email (an email-only Authorization with no
// provider_id, colliding on (provider, email) among sparse rows) as it
// already does for idx_users_provider_lookup (#718 above). The recovery
// branch classifies via dberrors.Classify(result.Error), which keys off the
// PgError SQLSTATE, not the constraint name, so a different constraint name
// alone should not change behavior -- this test exists to prove that, not
// because performSparseUserInsert needed a code change for it.
// SEM@87d1696b4bf3edbe042353cf7586a60de78c2028: validate sparse user insert recovers a sparse-email duplicate-key race by refetching the winning row
func TestPerformSparseUserInsert_SparseEmailDuplicateKeyRefetchesWinner(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	mock.MatchExpectationsInOrder(true)

	email := openapi_types.Email("carol@example.com")
	authEntry := &Authorization{
		PrincipalType: AuthorizationPrincipalTypeUser,
		Provider:      "google",
		// No ProviderId: an email-only sparse insert is exactly the case
		// idx_users_sparse_email exists to constrain (provider_user_id IS NULL).
		Email: &email,
	}

	// FirstOrCreate's own lookup: no existing row (this request lost the race
	// to a concurrent sparse insert that hasn't been visible to this SELECT
	// yet).
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"internal_uuid"}))

	// The INSERT collides with idx_users_sparse_email (unique on
	// provider + email restricted to provider_user_id IS NULL, #720) because
	// a concurrent email-only sparse insert won the race.
	mock.ExpectQuery(`INSERT INTO "users"`).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "idx_users_sparse_email", Message: "duplicate key value violates unique constraint \"idx_users_sparse_email\""})

	// Re-fetch after the classified duplicate: must query by (provider,
	// provider_user_id/email) only -- NOT by the leftover internal_uuid from
	// the failed insert above (guardedQueryMatcher fails the test if it does)
	// -- and returns the winner's row.
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(sqlmock.NewRows([]string{"internal_uuid", "provider", "provider_user_id", "email", "name"}).
			AddRow("uuid-winner", "google", nil, "carol@example.com", "Carol"))

	err := performSparseUserInsert(context.Background(), gdb, authEntry)
	require.NoError(t, err, "duplicate-key create conflict on idx_users_sparse_email must be recovered by re-fetching, not surfaced as an error")

	require.NoError(t, mock.ExpectationsWereMet())
}
