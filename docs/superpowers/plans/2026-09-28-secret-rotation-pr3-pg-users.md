# Secret Rotation PR 3: database credentials (#965) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the app connecting as the database master user: on PostgreSQL bootstrap a `tmi_owner` role with alternating login users `tmi_a`/`tmi_b`, rotate their passwords with zero errors, rotate the admin (master) credential the rotator itself uses, and on Oracle rotate the single app user with an accepted brief gap.

**Architecture:** Two new PR 1 rotations. `DBPasswordRotation` (name `db-password`) owns `TMI_DATABASE_URL`: an idempotent PostgreSQL bootstrap (roles, ownership transfer, schema grants) runs once when the URL still names a non-`tmi_x` user; afterwards each rotation gives the idle user a new password, points the URL at it, waits for the server roll plus `ConnMaxLifetime` (240 s) and margin, then sets the previous user `NOLOGIN`. `DBAdminPasswordRotation` (name `db-admin-password`) owns `tmi-rotator-admin`, a Secret the server never references (no roll), with a `pending_password` written before `ALTER ROLE` so a crash leaves a recoverable state. Oracle URLs take a single-user path (`ALTER USER ... IDENTIFIED BY ... REPLACE ...`). SQL runs through `database/sql` handles (pgx stdlib / godror) with quoted identifiers and literals, never string-formatted values.

**Tech Stack:** Go 1.26, `github.com/jackc/pgx/v5` (`pgx.Identifier{}.Sanitize()`), `github.com/lib/pq` (`pq.QuoteLiteral`), `github.com/godror/godror` (build tag `oracle`), PR 1 `internal/rotator`, Terraform (RDS `ignore_changes`), kustomize.

**Spec:** `docs/superpowers/specs/2026-09-28-secret-rotation-design.md` §4 and §5. Builds on PR 1 (`docs/superpowers/plans/2026-09-28-secret-rotation-pr1-rotator.md`): `rotator.Env`, `Rotation`, `Env.Transition`, `Env.WaitServerRolled`, `Env.Secrets` (`SecretStore.Get/Update`), `AnnPhase`, `AnnRotatedAt`, `AnnGeneration`, `NewPassword`, `MemorySecretStore`, `NewFakeRolloutWaiter`, `testEnv`, `cmd/rotator` `options`/`run`, `scripts/rotate-secret.py`, `deployments/k8s/dev/rotator.yml` (Role already names `tmi-rotator-admin`), `internal/secrets` `file` provider, `deploy-aws.sh import_config`.

## Open questions (resolved 2026-09-28)

HUMAN DECISIONS (Eric, 2026-09-28): every item below keeps the default the plan states (items 1-6 accepted as written). Related: (A) webhook secrets and the content-token key are out of #965 (separate issue), so nothing in this PR's DB-user work touches them.

1. **Ownership transfer scope.** `REASSIGN OWNED` is out (spec) because the RDS master owns objects we must not move. The plan transfers every relation, sequence, view, function and trigger function in schema `public` that the *current app user* owns and that AutoMigrate/`internal/dbschema` created. On PostgreSQL 15+ `public` itself is owned by `pg_database_owner`, so `tmi_owner` also needs `GRANT USAGE, CREATE ON SCHEMA public`. Anything outside `public` (extensions such as `vector`) stays with the master. If a future object lands outside `public`, the bootstrap must be extended: acceptable?
2. **`ALTER DEFAULT PRIVILEGES` is dropped.** With `ALTER ROLE tmi_a SET role = 'tmi_owner'`, every session of `tmi_a`/`tmi_b` runs as `tmi_owner`, so objects AutoMigrate creates are owned by `tmi_owner` directly and no default-privilege rule is needed. Spec lists it; the plan omits it and verifies the claim in the integration test (`TestDBRotationIntegration_BootstrapOwnership`).
3. **Password statements in server logs.** `ALTER ROLE ... PASSWORD` sends the password to the server; PostgreSQL logs it only if `log_statement` is `ddl`/`all` (RDS parameter group leaves the default `none`; verified) and Oracle's `IDENTIFIED BY` likewise only under audit of DDL. The plan pins `SET LOCAL log_statement = 'none'` inside the transaction on PostgreSQL as belt and braces (RDS master can set it per session). Alternative: pre-computed SCRAM verifiers; not done.
4. **Old-user sessions past `ConnMaxLifetime`.** After `NOLOGIN` the spec relies on the pool lifetime (240 s + margin). The plan also runs `pg_terminate_backend` for any leftover session of the retired user after the wait, so a stuck connection cannot outlive the rotation. Say if you prefer the pure wait.
5. **Oracle dev overlay.** The Oracle app user's URL lives in `tmi-oracle-db/database-url` (plus `oracle-password`), not `tmi-secrets`. The plan makes the DB rotation's Secret/key configurable (`TMI_ROTATOR_DB_SECRET`, `TMI_ROTATOR_DB_URL_KEY`) and wires the `docker-desktop-oracle` overlay accordingly; the Oracle rotator also needs the wallet mount. Since Oracle is dev/test only, this is the minimum that makes `make rotate-secret name=db-password` work there.
6. **RDS master recovery** (`aws rds modify-db-instance --master-user-password`) is a runbook step only; no automation.

## Global Constraints

Same as PR 1 (make targets only, slogging only, secret safety, SEM markers, Terraform/kustomize split, commit trailer). Plus:

- **Every SQL identifier is quoted** with `pgx.Identifier{name}.Sanitize()` (PostgreSQL) and **every password literal** with `pq.QuoteLiteral` (PostgreSQL) or the Oracle double-quote rule (`"..."`, reject passwords containing `"`); no `fmt.Sprintf` of a raw password into SQL. Passwords come from `rotator.NewPassword()` (alphanumeric, 32 chars), which satisfies both PostgreSQL and Oracle ADB policy (12+ chars, upper, lower, digit; enforced by `NewDBPassword`, Task 1).
- **`#nosec` not `//nolint:gosec`** for the unavoidable `G202` (SQL string concatenation) on the sanitized statements; directive on its own line above.
- **Oracle review is mandatory** (Task 8) for the whole PR: ownership transfer, GORM hooks, sequences, `IDENTIFIED BY ... REPLACE`.
- **PR title:** `feat(rotator): rotate database credentials with alternating PostgreSQL users (#965)`.
- **PostgreSQL 15+** (RDS `engine_version` default `16.4`, `terraform/modules/database/aws/variables.tf:9`): `public` is owned by `pg_database_owner`, so the master can `GRANT USAGE, CREATE ON SCHEMA public TO tmi_owner`; on PostgreSQL 14 and older that grant is unnecessary but harmless.
- **Startup DDL as `tmi_a`:** `internal/dbschema/postgres_isolation.go` runs `ALTER ROLE CURRENT_USER SET default_transaction_isolation` at boot; after `SET role`, `CURRENT_USER` is `tmi_owner` (a role may alter its own settings, so it succeeds) but role-level settings apply at LOGIN, so `Bootstrap` also pins the setting on `tmi_a` and `tmi_b` (Task 2). `rg -n "ALTER DATABASE|ALTER SYSTEM|CREATE EXTENSION" internal/dbschema api cmd --type go` finds nothing else that needs the master.
- **Oracle single-user gap** is accepted by decision 5: on Oracle, requests between the password change and the server roll may fail; integration tests on Oracle assert recovery, not zero errors.

## Review Focus

1. Bootstrap re-run after a partial failure (roles exist, ownership half-transferred): expected every statement is idempotent (`IF NOT EXISTS` / catalog checks) and the second run finishes without error (Task 7 `TestDBRotationIntegration_BootstrapOwnership`; the statement plan itself in Task 2 `TestPGBootstrapPlan_Order`).
2. `TMI_DATABASE_URL` with a URL-encoded password or query parameters (`sslmode=require`): expected the rewritten URL keeps every parameter and encodes the new password (Task 1 `TestRewriteURLUser_KeepsParams`).
3. Rotation while the idle user still has `LOGIN` from an interrupted previous run: expected the new password simply replaces it; no duplicate role errors (Task 4 `TestDBPasswordRotation_ResumeFromSwapped`).
4. Admin rotation crash between writing `pending_password` and `ALTER ROLE`: expected the next run tries pending then current, and whichever authenticates becomes the URL (Task 5 `TestDBAdminRotation_ResumePendingEitherWay`).
5. Server pods still holding connections as the retired user after `NOLOGIN`: expected those sessions keep working until they close (NOLOGIN affects new logins only), the pool recycles within 240 s, and the terminate step catches stragglers; no 500s in the API loop (Task 7 integration test).

---

## File map

| File | Change |
|---|---|
| `internal/rotator/dburl.go` (new) + test | parse `TMI_DATABASE_URL`, swap user/password, dialect detection, `NewDBPassword` |
| `internal/rotator/pg_admin.go` (new) + test | `PostgresAdmin`: bootstrap, set password, set login, terminate sessions, owned-object listing (`database/sql` over pgx stdlib) |
| `internal/rotator/oracle_admin.go` (+ `_oracle.go`/`_nooracle.go` build tags) | `OracleUserAdmin`: `ALTER USER ... IDENTIFIED BY ... REPLACE ...` |
| `internal/rotator/db_password.go` (new) + test | `DBPasswordRotation` |
| `internal/rotator/db_admin_password.go` (new) + test | `DBAdminPasswordRotation` |
| `cmd/rotator/main.go` | admin Secret, DB rotations, `TMI_ROTATOR_DB_*` options |
| `internal/config/process_env.go`, `config-reference.md` | new rotator env vars |
| `deployments/k8s/dev/rotator.yml`, `docker-desktop-oracle/kustomization.yaml` (+ patch) | admin Secret env; Oracle overlay wiring |
| `scripts/lib/deploy.py` | create `tmi-rotator-admin` on dev clusters |
| `terraform/modules/kubernetes/aws/k8s_resources.tf`, `terraform/modules/database/aws/main.tf`, `terraform/modules/secrets/aws/*`, `terraform/modules/kubernetes/aws/{main,variables}.tf`, `terraform/environments/aws-public/main.tf` | `tmi-rotator-admin` seed, RDS `ignore_changes = [password]`, drop the last Secrets Manager secret and the IRSA statement |
| `scripts/deploy-aws.sh` | `import_config` reads `TMI_DATABASE_URL` from `tmi-secrets` (file reference) |
| `test/integration/workflows/db_rotation_test.go` (new) | bootstrap ownership, rotation under load, Oracle recovery |
| wiki `Secret-Rotation.md`, `Database-Operations.md` | runbook: RDS master recovery, role model |

---

### Task 1: Passwords and URL surgery

**Files:**
- Create: `internal/rotator/dburl.go`
- Test: `internal/rotator/dburl_test.go`

**Interfaces (Produces):**

```go
type DBURL struct {
	Raw      string
	Scheme   string // "postgres", "postgresql", "oracle"
	User     string
	Password string
	parsed   *url.URL
}
func ParseDBURL(raw string) (*DBURL, error)
func (u *DBURL) IsPostgres() bool
func (u *DBURL) IsOracle() bool
func (u *DBURL) WithCredentials(user, password string) string // same URL, new userinfo, password url-encoded
func NewDBPassword() (string, error) // NewPassword() re-drawn until it has upper, lower and digit
const (
	PGUserA = "tmi_a"
	PGUserB = "tmi_b"
	PGOwnerRole = "tmi_owner"
)
func IdleUser(current string) (string, bool) // tmi_a -> tmi_b, tmi_b -> tmi_a, else ("", false)
```

- [ ] **Step 1: Write the failing tests**

```go
package rotator

import (
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
)

func TestRewriteURLUser_KeepsParams(t *testing.T) {
	u, err := ParseDBURL("postgresql://master:p%40ss%3Aword@db.example:5432/tmi?sslmode=require&application_name=tmi")
	require.NoError(t, err)
	require.True(t, u.IsPostgres())
	require.Equal(t, "master", u.User)
	require.Equal(t, "p@ss:word", u.Password)
	out := u.WithCredentials("tmi_a", "n3w/pass?")
	require.Equal(t, "postgresql://tmi_a:n3w%2Fpass%3F@db.example:5432/tmi?sslmode=require&application_name=tmi", out)
	back, err := ParseDBURL(out)
	require.NoError(t, err)
	require.Equal(t, "n3w/pass?", back.Password)
}

func TestParseDBURL_OracleAndErrors(t *testing.T) {
	u, err := ParseDBURL("oracle://app:secret@tmiadb_high")
	require.NoError(t, err)
	require.True(t, u.IsOracle())
	require.Equal(t, "oracle://app:other@tmiadb_high", u.WithCredentials("app", "other"))
	_, err = ParseDBURL("")
	require.Error(t, err)
	_, err = ParseDBURL("mysql://a:b@c/d")
	require.Error(t, err)
	_, err = ParseDBURL("postgres://nouser@host/db")
	require.Error(t, err, "user is required")
}

func TestIdleUser(t *testing.T) {
	idle, ok := IdleUser("tmi_a")
	require.True(t, ok)
	require.Equal(t, "tmi_b", idle)
	idle, ok = IdleUser("tmi_b")
	require.True(t, ok)
	require.Equal(t, "tmi_a", idle)
	_, ok = IdleUser("tmiadmin")
	require.False(t, ok)
}

func TestNewDBPassword_Policy(t *testing.T) {
	for i := 0; i < 50; i++ {
		p, err := NewDBPassword()
		require.NoError(t, err)
		require.Len(t, p, 32)
		var up, lo, di bool
		for _, r := range p {
			up = up || unicode.IsUpper(r)
			lo = lo || unicode.IsLower(r)
			di = di || unicode.IsDigit(r)
		}
		require.True(t, up && lo && di)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestRewriteURLUser count1=true`
Expected: `undefined: ParseDBURL`.

- [ ] **Step 3: Implement `dburl.go`**

```go
package rotator

import (
	"errors"
	"fmt"
	"net/url"
	"unicode"
)

const (
	PGUserA     = "tmi_a"
	PGUserB     = "tmi_b"
	PGOwnerRole = "tmi_owner"
)

// DBURL is a parsed TMI_DATABASE_URL with the credentials split out.
// SEM@<sha>: parsed database URL exposing scheme, user and password (pure)
type DBURL struct {
	Raw      string
	Scheme   string
	User     string
	Password string
	parsed   *url.URL
}

// SEM@<sha>: parse a postgres or oracle database URL, requiring a user (pure)
func ParseDBURL(raw string) (*DBURL, error) {
	if raw == "" {
		return nil, errors.New("database URL is empty")
	}
	p, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("database URL: %w", err)
	}
	switch p.Scheme {
	case "postgres", "postgresql", "oracle":
	default:
		return nil, fmt.Errorf("database URL scheme %q is not rotatable (postgres, postgresql, oracle)", p.Scheme)
	}
	if p.User == nil || p.User.Username() == "" {
		return nil, errors.New("database URL has no user")
	}
	pw, _ := p.User.Password()
	return &DBURL{Raw: raw, Scheme: p.Scheme, User: p.User.Username(), Password: pw, parsed: p}, nil
}

// SEM@<sha>: report whether the URL targets PostgreSQL (pure)
func (u *DBURL) IsPostgres() bool { return u.Scheme == "postgres" || u.Scheme == "postgresql" }

// SEM@<sha>: report whether the URL targets Oracle (pure)
func (u *DBURL) IsOracle() bool { return u.Scheme == "oracle" }

// WithCredentials returns the same URL with new userinfo; url.UserPassword
// percent-encodes the password on String().
// SEM@<sha>: rewrite the URL's user and password keeping host, path and query (pure)
func (u *DBURL) WithCredentials(user, password string) string {
	c := *u.parsed
	c.User = url.UserPassword(user, password)
	return c.String()
}

// IdleUser names the alternating user that is not in use.
// SEM@<sha>: return the other of tmi_a/tmi_b, false for any other user (pure)
func IdleUser(current string) (string, bool) {
	switch current {
	case PGUserA:
		return PGUserB, true
	case PGUserB:
		return PGUserA, true
	}
	return "", false
}

// NewDBPassword draws NewPassword until it satisfies Oracle ADB's policy
// (upper, lower, digit); PostgreSQL has no policy. Alphanumeric only, so it
// needs no quoting in a URL and cannot contain Oracle's forbidden '"'.
// SEM@<sha>: generate a database password satisfying Oracle and PostgreSQL rules (pure)
func NewDBPassword() (string, error) {
	for i := 0; i < 100; i++ {
		p, err := NewPassword()
		if err != nil {
			return "", err
		}
		var up, lo, di bool
		for _, r := range p {
			up = up || unicode.IsUpper(r)
			lo = lo || unicode.IsLower(r)
			di = di || unicode.IsDigit(r)
		}
		if up && lo && di {
			return p, nil
		}
	}
	return "", errors.New("could not draw a policy-compliant password")
}
```

- [ ] **Step 4: Run**

Run: `make test-unit name=TestRewriteURLUser count1=true`, `make test-unit name=TestParseDBURL count1=true`, `make test-unit name=TestIdleUser count1=true`, `make test-unit name=TestNewDBPassword count1=true`

- [ ] **Step 5: Commit**

```bash
git add internal/rotator/dburl.go internal/rotator/dburl_test.go
git commit -m "feat(rotator): database URL credential rewriting and policy-safe passwords (#965)"
```

---

### Task 2: PostgreSQL admin operations

**Files:**
- Create: `internal/rotator/pg_admin.go`
- Test: `internal/rotator/pg_admin_test.go` (SQL text assertions with a `sqlmock`-free approach: the functions build statements through a small `execer` interface; tests capture the statements)

**Interfaces (Produces):**

```go
// DBAdmin is what the rotations need from an administrative connection.
type DBAdmin interface {
	// Bootstrap makes tmi_owner/tmi_a/tmi_b exist, moves ownership of every
	// object appUser owns in schema public to tmi_owner, and grants the schema.
	// Idempotent. Returns the password set on tmi_a (first login user).
	Bootstrap(ctx context.Context, appUser string) (string, error)
	SetPassword(ctx context.Context, user, password string, login bool) error // ALTER ROLE ... [NO]LOGIN PASSWORD
	SetLogin(ctx context.Context, user string, login bool) error
	TerminateSessions(ctx context.Context, user string) (int64, error)
	Ping(ctx context.Context) error
	Close() error
}
type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}
func OpenPostgresAdmin(ctx context.Context, adminURL string) (*PostgresAdmin, error) // sql.Open("pgx", url)
func NewPostgresAdminWith(db *sql.DB) *PostgresAdmin
```

- [ ] **Step 1: Write the failing tests**

Use `github.com/DATA-DOG/go-sqlmock` if it is already in `go.mod` (`rg -n sqlmock go.mod`); if not, do not add it: test through a fake `execer` that records statements and answers catalog queries from canned rows via `sql.Rows` is awkward, so instead structure `PostgresAdmin` so every statement is built by pure functions and test those:

```go
package rotator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPGStatements_QuoteEverything(t *testing.T) {
	require.Equal(t, `ALTER ROLE "tmi_a" WITH LOGIN PASSWORD 'p''q'`, pgSetPasswordSQL("tmi_a", "p'q", true))
	require.Equal(t, `ALTER ROLE "tmi_b" WITH NOLOGIN`, pgSetLoginSQL("tmi_b", false))
	require.Equal(t, `ALTER TABLE "public"."users" OWNER TO "tmi_owner"`, pgOwnerSQL("r", "users"))
	require.Equal(t, `ALTER SEQUENCE "public"."tmi_threat_model_alias_seq" OWNER TO "tmi_owner"`, pgOwnerSQL("S", "tmi_threat_model_alias_seq"))
	require.Equal(t, `ALTER VIEW "public"."v" OWNER TO "tmi_owner"`, pgOwnerSQL("v", "v"))
	require.Equal(t, `ALTER FUNCTION "public"."tmi_audit_append_only_guard"() OWNER TO "tmi_owner"`, pgFunctionOwnerSQL("tmi_audit_append_only_guard", ""))
	require.Equal(t, `ALTER TABLE "public"."weird""name" OWNER TO "tmi_owner"`, pgOwnerSQL("r", `weird"name`))
}

func TestPGBootstrapPlan_Order(t *testing.T) {
	steps := pgBootstrapSQL("master", "PwA1", "PwB2")
	require.Equal(t, []string{
		`SELECT 1 FROM pg_roles WHERE rolname = 'tmi_owner'`,
		`CREATE ROLE "tmi_owner" NOLOGIN`,
		`GRANT "tmi_owner" TO "master"`,
		`SELECT 1 FROM pg_roles WHERE rolname = 'tmi_a'`,
		`CREATE ROLE "tmi_a" NOLOGIN IN ROLE "tmi_owner"`,
		`ALTER ROLE "tmi_a" WITH LOGIN PASSWORD 'PwA1'`,
		`ALTER ROLE "tmi_a" SET role = 'tmi_owner'`,
		`ALTER ROLE "tmi_a" SET default_transaction_isolation = 'serializable'`,
		`SELECT 1 FROM pg_roles WHERE rolname = 'tmi_b'`,
		`CREATE ROLE "tmi_b" NOLOGIN IN ROLE "tmi_owner"`,
		`ALTER ROLE "tmi_b" WITH NOLOGIN PASSWORD 'PwB2'`,
		`ALTER ROLE "tmi_b" SET role = 'tmi_owner'`,
		`ALTER ROLE "tmi_b" SET default_transaction_isolation = 'serializable'`,
		`GRANT USAGE, CREATE ON SCHEMA "public" TO "tmi_owner"`,
	}, steps.statements())
}
```

(`pgBootstrapSQL` returns an ordered list of `{check, stmt}` pairs: `check` is a `SELECT 1` whose empty result triggers `stmt`; `statements()` flattens for the test.)

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestPGStatements count1=true`
Expected: undefined functions.

- [ ] **Step 3: Implement `pg_admin.go`**

```go
package rotator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
	"github.com/lib/pq"

	"github.com/ericfitz/tmi/internal/slogging"
)

// DBAdmin is the administrative surface the DB rotations use.
// SEM@<sha>: administrative database operations for credential rotation
type DBAdmin interface {
	Bootstrap(ctx context.Context, appUser string) (string, error)
	SetPassword(ctx context.Context, user, password string, login bool) error
	SetLogin(ctx context.Context, user string, login bool) error
	TerminateSessions(ctx context.Context, user string) (int64, error)
	Ping(ctx context.Context) error
	Close() error
}

// PostgresAdmin implements DBAdmin over database/sql with the pgx driver.
// SEM@<sha>: DBAdmin for PostgreSQL over a database/sql handle (writes DB)
type PostgresAdmin struct{ db *sql.DB }

// SEM@<sha>: open an administrative PostgreSQL connection from a URL
func OpenPostgresAdmin(ctx context.Context, adminURL string) (*PostgresAdmin, error) {
	db, err := sql.Open("pgx", adminURL)
	if err != nil {
		return nil, fmt.Errorf("open admin connection: %w", err)
	}
	db.SetMaxOpenConns(2)
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := db.PingContext(pctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("admin connection: %w", err)
	}
	return &PostgresAdmin{db: db}, nil
}

// SEM@<sha>: wrap an existing sql.DB as a PostgresAdmin (pure)
func NewPostgresAdminWith(db *sql.DB) *PostgresAdmin { return &PostgresAdmin{db: db} }

// SEM@<sha>: check the admin connection
func (p *PostgresAdmin) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }

// SEM@<sha>: close the admin connection
func (p *PostgresAdmin) Close() error { return p.db.Close() }

func ident(parts ...string) string { return pgx.Identifier(parts).Sanitize() }

// SEM@<sha>: build ALTER ROLE ... [NO]LOGIN PASSWORD with quoted identifier and literal (pure)
func pgSetPasswordSQL(user, password string, login bool) string {
	l := "NOLOGIN"
	if login {
		l = "LOGIN"
	}
	return "ALTER ROLE " + ident(user) + " WITH " + l + " PASSWORD " + pq.QuoteLiteral(password)
}

// SEM@<sha>: build ALTER ROLE ... [NO]LOGIN (pure)
func pgSetLoginSQL(user string, login bool) string {
	l := "NOLOGIN"
	if login {
		l = "LOGIN"
	}
	return "ALTER ROLE " + ident(user) + " WITH " + l
}

// SEM@<sha>: build the OWNER TO statement for a pg_class relkind (pure)
func pgOwnerSQL(relkind, name string) string {
	kind := map[string]string{"r": "TABLE", "p": "TABLE", "S": "SEQUENCE", "v": "VIEW", "m": "MATERIALIZED VIEW"}[relkind]
	if kind == "" {
		kind = "TABLE"
	}
	return "ALTER " + kind + " " + ident("public", name) + " OWNER TO " + ident(PGOwnerRole)
}

// SEM@<sha>: build ALTER FUNCTION ... OWNER TO for a function with its argument list (pure)
func pgFunctionOwnerSQL(name, args string) string {
	return "ALTER FUNCTION " + ident("public", name) + "(" + args + ") OWNER TO " + ident(PGOwnerRole)
}

type pgStep struct {
	check string // SELECT 1 ...; empty check = always run
	stmt  string
}

type pgSteps []pgStep

func (s pgSteps) statements() []string {
	var out []string
	for _, st := range s {
		if st.check != "" {
			out = append(out, st.check)
		}
		out = append(out, st.stmt)
	}
	return out
}

// SEM@<sha>: build the idempotent role bootstrap plan for PostgreSQL (pure)
func pgBootstrapSQL(adminUser, passwordA, passwordB string) pgSteps {
	roleExists := func(r string) string { return "SELECT 1 FROM pg_roles WHERE rolname = " + pq.QuoteLiteral(r) }
	return pgSteps{
		{roleExists(PGOwnerRole), "CREATE ROLE " + ident(PGOwnerRole) + " NOLOGIN"},
		{"", "GRANT " + ident(PGOwnerRole) + " TO " + ident(adminUser)},
		// CREATE is guarded (roles are cluster-wide and outlive DROP DATABASE);
		// the password/login ALTERs are unconditional so a re-run always returns
		// tmi_a's real password and clears a NOLOGIN left by an earlier rotation.
		{roleExists(PGUserA), "CREATE ROLE " + ident(PGUserA) + " NOLOGIN IN ROLE " + ident(PGOwnerRole)},
		{"", pgSetPasswordSQL(PGUserA, passwordA, true)},
		{"", "ALTER ROLE " + ident(PGUserA) + " SET role = " + pq.QuoteLiteral(PGOwnerRole)},
		// #450 pins default_transaction_isolation per LOGIN role; a role-level
		// SET applies at login, so tmi_owner's setting would not reach tmi_a.
		{"", "ALTER ROLE " + ident(PGUserA) + " SET default_transaction_isolation = 'serializable'"},
		{roleExists(PGUserB), "CREATE ROLE " + ident(PGUserB) + " NOLOGIN IN ROLE " + ident(PGOwnerRole)},
		{"", pgSetPasswordSQL(PGUserB, passwordB, false)},
		{"", "ALTER ROLE " + ident(PGUserB) + " SET role = " + pq.QuoteLiteral(PGOwnerRole)},
		{"", "ALTER ROLE " + ident(PGUserB) + " SET default_transaction_isolation = 'serializable'"},
		{"", "GRANT USAGE, CREATE ON SCHEMA " + ident("public") + " TO " + ident(PGOwnerRole)},
	}
}

// Bootstrap creates the role model and moves ownership. Runs in one
// transaction with statement logging off so passwords never reach the server log.
// SEM@<sha>: idempotently create tmi_owner/tmi_a/tmi_b and transfer public-schema ownership (writes DB)
func (p *PostgresAdmin) Bootstrap(ctx context.Context, appUser string) (string, error) {
	logger := slogging.Get()
	passwordA, err := NewDBPassword()
	if err != nil {
		return "", err
	}
	passwordB, err := NewDBPassword()
	if err != nil {
		return "", err
	}
	var adminUser string
	if err := p.db.QueryRowContext(ctx, "SELECT current_user").Scan(&adminUser); err != nil {
		return "", err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET LOCAL log_statement = 'none'"); err != nil {
		logger.Warn("Could not disable statement logging for the bootstrap transaction: %v", err)
	}
	for _, st := range pgBootstrapSQL(adminUser, passwordA, passwordB) {
		if st.check != "" {
			var one int
			err := tx.QueryRowContext(ctx, st.check).Scan(&one)
			if err == nil {
				continue // exists
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return "", fmt.Errorf("bootstrap check: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, st.stmt); err != nil { // #nosec G202 -- identifiers sanitized, literals quoted
			return "", fmt.Errorf("bootstrap: %w", err)
		}
	}
	// Ownership: everything appUser owns in public.
	rows, err := tx.QueryContext(ctx, `
		SELECT c.relkind::text, c.relname FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace JOIN pg_roles r ON r.oid = c.relowner
		WHERE n.nspname = 'public' AND r.rolname = $1 AND c.relkind IN ('r','p','S','v','m')
		ORDER BY c.relname`, appUser)
	if err != nil {
		return "", err
	}
	var owners []string
	for rows.Next() {
		var kind, name string
		if err := rows.Scan(&kind, &name); err != nil {
			_ = rows.Close()
			return "", err
		}
		owners = append(owners, pgOwnerSQL(kind, name))
	}
	_ = rows.Close()
	frows, err := tx.QueryContext(ctx, `
		SELECT p.proname, pg_get_function_identity_arguments(p.oid) FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace JOIN pg_roles r ON r.oid = p.proowner
		WHERE n.nspname = 'public' AND r.rolname = $1 ORDER BY p.proname`, appUser)
	if err != nil {
		return "", err
	}
	for frows.Next() {
		var name, args string
		if err := frows.Scan(&name, &args); err != nil {
			_ = frows.Close()
			return "", err
		}
		owners = append(owners, pgFunctionOwnerSQL(name, args))
	}
	_ = frows.Close()
	for _, stmt := range owners {
		if _, err := tx.ExecContext(ctx, stmt); err != nil { // #nosec G202 -- identifiers sanitized
			return "", fmt.Errorf("ownership transfer: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	logger.Info("PostgreSQL role model bootstrapped objects_transferred=%d owner=%s", len(owners), PGOwnerRole)
	return passwordA, nil
}

// SEM@<sha>: set a role's password and login flag without logging the statement (writes DB)
func (p *PostgresAdmin) SetPassword(ctx context.Context, user, password string, login bool) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, _ = tx.ExecContext(ctx, "SET LOCAL log_statement = 'none'")
	if _, err := tx.ExecContext(ctx, pgSetPasswordSQL(user, password, login)); err != nil { // #nosec G202 -- sanitized
		return fmt.Errorf("set password for %s: %w", user, err)
	}
	return tx.Commit()
}

// SEM@<sha>: toggle a role's LOGIN attribute (writes DB)
func (p *PostgresAdmin) SetLogin(ctx context.Context, user string, login bool) error {
	_, err := p.db.ExecContext(ctx, pgSetLoginSQL(user, login)) // #nosec G202 -- sanitized
	return err
}

// SEM@<sha>: terminate every other session of a role, returning the count (writes DB)
func (p *PostgresAdmin) TerminateSessions(ctx context.Context, user string) (int64, error) {
	var n int64
	err := p.db.QueryRowContext(ctx, `
		SELECT count(*) FROM (SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		WHERE usename = $1 AND pid <> pg_backend_pid()) t`, user).Scan(&n)
	return n, err
}
```

If `github.com/jackc/pgx/v5/stdlib` is not already a direct dependency, `go mod tidy` (through `make lint`) adds it; pgx v5 is already required.

- [ ] **Step 4: Run**

Run: `make test-unit name=TestPGStatements count1=true`, `make test-unit name=TestPGBootstrapPlan count1=true`, `make lint`.

- [ ] **Step 5: Commit**

```bash
git add internal/rotator/pg_admin.go internal/rotator/pg_admin_test.go go.mod go.sum
git commit -m "feat(rotator): PostgreSQL role bootstrap and password administration (#965)"
```

---
### Task 3: Oracle single-user admin

**Files:**
- Create: `internal/rotator/oracle_admin.go` (build tag `oracle`), `internal/rotator/oracle_admin_nooracle.go` (build tag `!oracle`)
- Test: `internal/rotator/oracle_admin_test.go` (statement builder only; live Oracle is Task 7 / `make test-integration-oci`)

**Interfaces (Produces):**

```go
// OracleUserAdmin changes the app user's own password over its own connection.
type OracleUserAdmin interface {
	ChangeOwnPassword(ctx context.Context, user, oldPassword, newPassword string) error
	Close() error
}
func OpenOracleUserAdmin(ctx context.Context, appURL, walletDir string) (OracleUserAdmin, error) // godror; !oracle build returns an error
func oracleAlterUserSQL(user, newPassword, oldPassword string) (string, error) // rejects '"' in either password
```

- [ ] **Step 1: Write the failing test**

```go
package rotator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOracleAlterUserSQL(t *testing.T) {
	s, err := oracleAlterUserSQL("TMI_APP", "NewPass1", "OldPass2")
	require.NoError(t, err)
	require.Equal(t, `ALTER USER "TMI_APP" IDENTIFIED BY "NewPass1" REPLACE "OldPass2"`, s)
	_, err = oracleAlterUserSQL("TMI_APP", `bad"pw`, "x")
	require.Error(t, err)
	_, err = oracleAlterUserSQL(`bad"user`, "x", "y")
	require.Error(t, err)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestOracleAlterUserSQL count1=true`
Expected: undefined.

- [ ] **Step 3: Implement**

Shared (no build tag) in `oracle_admin_sql.go`:

```go
package rotator

import (
	"context"
	"fmt"
	"strings"
)

// OracleUserAdmin rotates an Oracle user's password with its own credentials.
// SEM@<sha>: change an Oracle user's own password
type OracleUserAdmin interface {
	ChangeOwnPassword(ctx context.Context, user, oldPassword, newPassword string) error
	Close() error
}

// oracleAlterUserSQL builds ALTER USER ... IDENTIFIED BY ... REPLACE ...; Oracle
// takes passwords as quoted identifiers, so a double quote cannot be escaped.
// SEM@<sha>: build the Oracle self-service password change statement, rejecting double quotes (pure)
func oracleAlterUserSQL(user, newPassword, oldPassword string) (string, error) {
	for _, v := range []string{user, newPassword, oldPassword} {
		if v == "" || strings.ContainsAny(v, "\"\x00") {
			return "", fmt.Errorf("oracle identifier or password is empty or contains a double quote")
		}
	}
	return fmt.Sprintf(`ALTER USER "%s" IDENTIFIED BY "%s" REPLACE "%s"`, user, newPassword, oldPassword), nil
}
```

`oracle_admin.go` (`//go:build oracle`):

```go
package rotator

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/godror/godror"

	"github.com/ericfitz/tmi/auth/db"
)

type oracleUserAdmin struct{ sqlDB *sql.DB }

// SEM@<sha>: open the app user's own Oracle connection for a password change
func OpenOracleUserAdmin(ctx context.Context, appURL, walletDir string) (OracleUserAdmin, error) {
	cfg, err := db.ParseDatabaseURL(appURL)
	if err != nil {
		return nil, err
	}
	if walletDir != "" {
		cfg.OracleWalletLocation = walletDir
	}
	dsn := db.OracleDSN(*cfg) // extract from auth/db/gorm_oracle.go getOracleDialector if not exported yet
	sqlDB, err := sql.Open("godror", dsn)
	if err != nil {
		return nil, err
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("oracle app connection: %w", err)
	}
	return &oracleUserAdmin{sqlDB: sqlDB}, nil
}

// SEM@<sha>: run ALTER USER ... IDENTIFIED BY ... REPLACE ... as the user itself (writes DB)
func (o *oracleUserAdmin) ChangeOwnPassword(ctx context.Context, user, oldPassword, newPassword string) error {
	stmt, err := oracleAlterUserSQL(user, newPassword, oldPassword)
	if err != nil {
		return err
	}
	_, err = o.sqlDB.ExecContext(ctx, stmt) // #nosec G202 -- validated: no quotes possible
	return err
}

func (o *oracleUserAdmin) Close() error { return o.sqlDB.Close() }
```

`oracle_admin_nooracle.go` (`//go:build !oracle`): `OpenOracleUserAdmin` returns `errors.New("oracle support not compiled in (build with -tags oracle)")`. Check `auth/db/gorm_oracle.go` for the DSN builder (`rg -n "godror|dsn|ConnectString" auth/db/gorm_oracle.go`) and export it as `OracleDSN(cfg GormConfig) string` if it is inline. Oracle user names are stored upper-case; use the URL's user as given (the same string the server uses).

- [ ] **Step 4: Run**

Run: `make test-unit name=TestOracleAlterUserSQL count1=true`, `make build-rotator`, and `make build-dbtool-oci` to prove the `oracle` tag compiles (needs `scripts/oci-env.sh`; skip with a note if unavailable and rely on `make test-integration-oci` in Task 7).

- [ ] **Step 5: Commit**

```bash
git add internal/rotator/oracle_admin*.go auth/db/gorm_oracle.go
git commit -m "feat(rotator): Oracle self-service password change (#965)"
```

---

### Task 4: `DBPasswordRotation`

**Files:**
- Create: `internal/rotator/db_password.go`
- Test: `internal/rotator/db_password_test.go`

**Interfaces:**
- Consumes: PR 1 `Env`, `Transition`, `WaitServerRolled`; Task 1 `ParseDBURL`, `IdleUser`, `NewDBPassword`; Task 2 `DBAdmin`; Task 3 `OracleUserAdmin`.
- Produces: `NewDBPasswordRotation(opts DBPasswordOptions) *DBPasswordRotation`, `Name() == "db-password"`;

```go
type DBPasswordOptions struct {
	DBSecret     string        // Secret holding the app URL (default tmi-secrets); may differ from Env.SecretName (Oracle overlay)
	URLKey       string        // key inside it (default TMI_DATABASE_URL)
	ExtraKeys    map[string]string // Oracle: {"oracle-password": "<password only>"} keys to keep in sync; value template "password"
	OpenPostgres func(ctx context.Context, adminURL string) (DBAdmin, error)
	OpenOracle   func(ctx context.Context, appURL string) (OracleUserAdmin, error)
	AdminSecret  string        // tmi-rotator-admin
	AdminURLKey  string        // TMI_DB_ADMIN_URL
	PoolDrain    time.Duration // ConnMaxLifetime + margin, default 5m
	Sleep        func(ctx context.Context, d time.Duration) error
}
```

Annotations: `AnnPhase`, `AnnGeneration`, `AnnRotatedAt` (from PR 1) plus `tmi.dev/rotation-retire.db-password` = the user to `NOLOGIN` after the drain (a user name, not a secret; reuses PR 1's `AnnRetire` prefix).

Phases, PostgreSQL:

| phase | on entry | write | next |
|---|---|---|---|
| `""`, URL user not `tmi_a/tmi_b` | `Bootstrap(appUser)` -> passwordA | URL = `tmi_a`/passwordA; retire = "" (master keeps LOGIN) | `swapped` |
| `""`, URL user is `tmi_x` | idle = other; `SetPassword(idle, NEW, login=true)` | URL = idle/NEW; retire = old user | `swapped` |
| `swapped` | wait server rolled; sleep `PoolDrain`; if retire != "": `SetLogin(retire,false)`, `TerminateSessions(retire)` | annotations only: phase cleared, rotated-at, retire removed | done |

Phases, Oracle: `""` -> `ChangeOwnPassword(user, old, NEW)` (as the app user) -> write URL with NEW (+ `ExtraKeys`), phase `swapped` -> wait rolled -> done. The gap between the ALTER and the roll is the accepted exception.

- [ ] **Step 1: Write the failing tests**

```go
package rotator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeDBAdmin struct {
	bootstrapped []string
	passwords    map[string]string
	login        map[string]bool
	terminated   []string
	failPing     bool
}

func newFakeDBAdmin() *fakeDBAdmin {
	return &fakeDBAdmin{passwords: map[string]string{}, login: map[string]bool{}}
}
func (f *fakeDBAdmin) Bootstrap(_ context.Context, appUser string) (string, error) {
	f.bootstrapped = append(f.bootstrapped, appUser)
	f.passwords[PGUserA] = "bootA"
	f.login[PGUserA] = true
	return "bootA", nil
}
func (f *fakeDBAdmin) SetPassword(_ context.Context, user, pw string, login bool) error {
	f.passwords[user] = pw
	f.login[user] = login
	return nil
}
func (f *fakeDBAdmin) SetLogin(_ context.Context, user string, login bool) error { f.login[user] = login; return nil }
func (f *fakeDBAdmin) TerminateSessions(_ context.Context, user string) (int64, error) {
	f.terminated = append(f.terminated, user)
	return 1, nil
}
func (f *fakeDBAdmin) Ping(context.Context) error {
	if f.failPing {
		return errors.New("auth failed")
	}
	return nil
}
func (f *fakeDBAdmin) Close() error { return nil }

func dbTestEnv(url string) (*Env, *MemorySecretStore, *fakeDBAdmin, *DBPasswordRotation) {
	env, _ := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{}, Annotations: map[string]string{}})
	// One store serves both Secrets.
	env.Secrets = NewMemorySecretStore(
		&Secret{Name: "tmi-secrets", Data: map[string]string{"TMI_DATABASE_URL": url}, Annotations: map[string]string{}},
		&Secret{Name: "tmi-rotator-admin", Data: map[string]string{"TMI_DB_ADMIN_URL": "postgres://master:mpw@db:5432/tmi?sslmode=require"}},
	)
	st := env.Secrets.(*MemorySecretStore)
	env.Rollouts = NewFakeRolloutWaiter(st, "tmi-server")
	admin := newFakeDBAdmin()
	var slept []time.Duration
	r := NewDBPasswordRotation(DBPasswordOptions{
		DBSecret: "tmi-secrets", URLKey: "TMI_DATABASE_URL",
		AdminSecret: "tmi-rotator-admin", AdminURLKey: "TMI_DB_ADMIN_URL",
		OpenPostgres: func(context.Context, string) (DBAdmin, error) { return admin, nil },
		PoolDrain:    5 * time.Minute,
		Sleep:        func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
	})
	return env, st, admin, r
}

func TestDBPasswordRotation_BootstrapsFromMaster(t *testing.T) {
	env, st, admin, r := dbTestEnv("postgres://master:mpw@db:5432/tmi?sslmode=require")
	require.NoError(t, r.Run(context.Background(), env))
	require.Equal(t, []string{"master"}, admin.bootstrapped)
	s, _ := st.Get(context.Background(), "tmi-secrets")
	u, err := ParseDBURL(s.Data["TMI_DATABASE_URL"])
	require.NoError(t, err)
	require.Equal(t, PGUserA, u.User)
	require.Equal(t, "bootA", u.Password)
	require.Contains(t, s.Data["TMI_DATABASE_URL"], "sslmode=require")
	require.Empty(t, s.Annotations[AnnPhase+"db-password"])
	_, touched := admin.login["master"]
	require.False(t, touched, "master login untouched")
	require.Empty(t, admin.terminated, "nothing retired on bootstrap")
	require.Equal(t, 1, st.DataWrites)
}

func TestDBPasswordRotation_AlternatesUsers(t *testing.T) {
	env, st, admin, r := dbTestEnv("postgres://tmi_a:pwA@db:5432/tmi?sslmode=require")
	admin.login[PGUserA] = true
	require.NoError(t, r.Run(context.Background(), env))
	s, _ := st.Get(context.Background(), "tmi-secrets")
	u, _ := ParseDBURL(s.Data["TMI_DATABASE_URL"])
	require.Equal(t, PGUserB, u.User)
	require.Equal(t, admin.passwords[PGUserB], u.Password)
	require.True(t, admin.login[PGUserB])
	require.False(t, admin.login[PGUserA], "old user NOLOGIN after the drain")
	require.Equal(t, []string{PGUserA}, admin.terminated)
	require.Empty(t, s.Annotations[AnnPhase+"db-password"])
	require.NotEmpty(t, s.Annotations[AnnRotatedAt+"db-password"])
	require.Empty(t, admin.bootstrapped)
}

func TestDBPasswordRotation_ResumeFromSwapped(t *testing.T) {
	env, st, admin, r := dbTestEnv("postgres://tmi_b:pwB@db:5432/tmi?sslmode=require")
	s, _ := st.Get(context.Background(), "tmi-secrets")
	s.Annotations[AnnPhase+"db-password"] = "swapped"
	s.Annotations[AnnRetire+"db-password"] = PGUserA
	s.Annotations[AnnGeneration+"db-password"] = "0"
	require.NoError(t, st.Update(context.Background(), s))
	st.DataWrites = 1
	admin.login[PGUserA] = true
	admin.login[PGUserB] = true
	require.NoError(t, r.Run(context.Background(), env))
	require.False(t, admin.login[PGUserA])
	require.True(t, admin.login[PGUserB])
	s, _ = st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"db-password"])
	_, retire := s.Annotations[AnnRetire+"db-password"]
	require.False(t, retire)
}

func TestDBPasswordRotation_RolloutFailureKeepsOldUserLogin(t *testing.T) {
	env, st, admin, r := dbTestEnv("postgres://tmi_a:pwA@db:5432/tmi?sslmode=require")
	s, _ := st.Get(context.Background(), "tmi-secrets")
	s.Annotations[AnnPhase+"db-password"] = "swapped"
	s.Annotations[AnnRetire+"db-password"] = PGUserA
	s.Annotations[AnnGeneration+"db-password"] = "9" // never rolled
	require.NoError(t, st.Update(context.Background(), s))
	admin.login[PGUserA] = true
	require.Error(t, r.Run(context.Background(), env))
	require.True(t, admin.login[PGUserA])
	require.Empty(t, admin.terminated)
}

type fakeOracle struct{ calls [][3]string }

func (f *fakeOracle) ChangeOwnPassword(_ context.Context, u, o, n string) error {
	f.calls = append(f.calls, [3]string{u, o, n})
	return nil
}
func (f *fakeOracle) Close() error { return nil }

func TestDBPasswordRotation_OracleSingleUser(t *testing.T) {
	env, _, _, _ := dbTestEnv("postgres://x:y@z/d")
	st := NewMemorySecretStore(&Secret{Name: "tmi-oracle-db", Data: map[string]string{"database-url": "oracle://TMI_APP:OldPass1@tmiadb_high", "oracle-password": "OldPass1"}, Annotations: map[string]string{}})
	env.Secrets = st
	env.Rollouts = NewFakeRolloutWaiter(st, "tmi-server")
	env.SecretName = "tmi-oracle-db"
	ora := &fakeOracle{}
	r := NewDBPasswordRotation(DBPasswordOptions{
		DBSecret: "tmi-oracle-db", URLKey: "database-url", ExtraKeys: map[string]string{"oracle-password": "password"},
		OpenOracle: func(context.Context, string) (OracleUserAdmin, error) { return ora, nil },
		PoolDrain:  time.Minute, Sleep: func(context.Context, time.Duration) error { return nil },
	})
	require.NoError(t, r.Run(context.Background(), env))
	require.Len(t, ora.calls, 1)
	require.Equal(t, "TMI_APP", ora.calls[0][0])
	require.Equal(t, "OldPass1", ora.calls[0][1])
	s, _ := st.Get(context.Background(), "tmi-oracle-db")
	u, _ := ParseDBURL(s.Data["database-url"])
	require.Equal(t, ora.calls[0][2], u.Password)
	require.Equal(t, ora.calls[0][2], s.Data["oracle-password"])
	require.Empty(t, s.Annotations[AnnPhase+"db-password"])
}
```

Note: the phase annotations of `db-password` live on the Secret that holds the URL (`DBSecret`), which for Oracle is `tmi-oracle-db`, so `Env.SecretName` must equal `DBSecret` for `Transition` to work; `cmd/rotator` (Task 6) runs this rotation with a copy of `Env` whose `SecretName = DBSecret`.

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestDBPasswordRotation count1=true`
Expected: `undefined: NewDBPasswordRotation`.

- [ ] **Step 3: Implement `db_password.go`**

```go
package rotator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	dbRotationName    = "db-password"
	dbPhaseSwapped    = "swapped"
	DefaultPoolDrain  = 5 * time.Minute // ConnMaxLifetime (240s) + margin
	AdminURLKey       = "TMI_DB_ADMIN_URL"
	AdminPendingKey   = "TMI_DB_ADMIN_PENDING_PASSWORD"
	AdminSecretName   = "tmi-rotator-admin"
)

// DBPasswordOptions configures which Secret holds the app URL and how to reach the database.
// SEM@<sha>: settings for the database password rotation (pure)
type DBPasswordOptions struct {
	DBSecret     string
	URLKey       string
	ExtraKeys    map[string]string
	OpenPostgres func(ctx context.Context, adminURL string) (DBAdmin, error)
	OpenOracle   func(ctx context.Context, appURL string) (OracleUserAdmin, error)
	AdminSecret  string
	AdminURLKey  string
	PoolDrain    time.Duration
	Sleep        func(ctx context.Context, d time.Duration) error
}

// DBPasswordRotation rotates the application's database credential.
// SEM@<sha>: phased rotation of the app database credential: alternating users on PostgreSQL, self-change on Oracle
type DBPasswordRotation struct{ o DBPasswordOptions }

// SEM@<sha>: build a DBPasswordRotation with defaults filled in (pure)
func NewDBPasswordRotation(o DBPasswordOptions) *DBPasswordRotation {
	if o.DBSecret == "" {
		o.DBSecret = "tmi-secrets"
	}
	if o.URLKey == "" {
		o.URLKey = "TMI_DATABASE_URL"
	}
	if o.AdminSecret == "" {
		o.AdminSecret = AdminSecretName
	}
	if o.AdminURLKey == "" {
		o.AdminURLKey = AdminURLKey
	}
	if o.PoolDrain == 0 {
		o.PoolDrain = DefaultPoolDrain
	}
	if o.Sleep == nil {
		o.Sleep = sleepCtx
	}
	return &DBPasswordRotation{o: o}
}

// SEM@<sha>: context-aware sleep
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// SEM@<sha>: return the rotation name (pure)
func (r *DBPasswordRotation) Name() string { return dbRotationName }

// SEM@<sha>: advance the database password rotation from its recorded phase
func (r *DBPasswordRotation) Run(ctx context.Context, env *Env) error {
	if env.SecretName != r.o.DBSecret {
		return fmt.Errorf("db-password rotation needs Env.SecretName == %q (got %q)", r.o.DBSecret, env.SecretName)
	}
	s, err := env.Secrets.Get(ctx, r.o.DBSecret)
	if err != nil {
		return err
	}
	u, err := ParseDBURL(s.Data[r.o.URLKey])
	if err != nil {
		return fmt.Errorf("%s/%s: %w", r.o.DBSecret, r.o.URLKey, err)
	}
	if u.IsOracle() {
		return r.runOracle(ctx, env, s, u)
	}
	return r.runPostgres(ctx, env, s, u)
}

// SEM@<sha>: PostgreSQL path: bootstrap or alternate users, then retire the old one after the drain
func (r *DBPasswordRotation) runPostgres(ctx context.Context, env *Env, s *Secret, u *DBURL) error {
	logger := slogging.Get()
	name := r.Name()
	phase := s.Annotations[AnnPhase+name]

	if phase == "" {
		adminSec, err := env.Secrets.Get(ctx, r.o.AdminSecret)
		if err != nil {
			return fmt.Errorf("admin credential: %w", err)
		}
		admin, err := r.o.OpenPostgres(ctx, adminSec.Data[r.o.AdminURLKey])
		if err != nil {
			return err
		}
		defer func() { _ = admin.Close() }()

		var newUser, newPassword, retire string
		if idle, ok := IdleUser(u.User); ok {
			pw, err := NewDBPassword()
			if err != nil {
				return err
			}
			if err := admin.SetPassword(ctx, idle, pw, true); err != nil {
				return err
			}
			newUser, newPassword, retire = idle, pw, u.User
			logger.Info("Database idle user enabled user=%s", idle)
		} else {
			pw, err := admin.Bootstrap(ctx, u.User)
			if err != nil {
				return fmt.Errorf("bootstrap role model: %w", err)
			}
			newUser, newPassword, retire = PGUserA, pw, "" // the master keeps LOGIN
			logger.Info("Database role model bootstrapped; app moves from %s to %s", u.User, PGUserA)
		}
		if err := env.Transition(ctx, name, dbPhaseSwapped, func(s *Secret) {
			s.Data[r.o.URLKey] = u.WithCredentials(newUser, newPassword)
			if retire != "" {
				s.Annotations[AnnRetire+name] = retire
			} else {
				delete(s.Annotations, AnnRetire+name)
			}
		}); err != nil {
			return err
		}
		if s, err = env.Secrets.Get(ctx, r.o.DBSecret); err != nil {
			return err
		}
		phase = dbPhaseSwapped
	}

	if phase != dbPhaseSwapped {
		return fmt.Errorf("unknown %s phase %q", name, phase)
	}
	if err := env.WaitServerRolled(ctx, s, name); err != nil {
		return fmt.Errorf("waiting for %s to reconnect with the new database user: %w", env.ServerDeployment, err)
	}
	if retire := s.Annotations[AnnRetire+name]; retire != "" {
		logger.Info("Draining pooled connections of user=%s for %s", retire, r.o.PoolDrain)
		if err := r.o.Sleep(ctx, r.o.PoolDrain); err != nil {
			return err
		}
		adminSec, err := env.Secrets.Get(ctx, r.o.AdminSecret)
		if err != nil {
			return err
		}
		admin, err := r.o.OpenPostgres(ctx, adminSec.Data[r.o.AdminURLKey])
		if err != nil {
			return err
		}
		defer func() { _ = admin.Close() }()
		if err := admin.SetLogin(ctx, retire, false); err != nil {
			return err
		}
		n, err := admin.TerminateSessions(ctx, retire)
		if err != nil {
			return err
		}
		logger.Info("Database user retired user=%s terminated_sessions=%d", retire, n)
	}
	return env.Transition(ctx, name, "", func(s *Secret) { delete(s.Annotations, AnnRetire+name) })
}

// SEM@<sha>: Oracle path: the app user changes its own password, then the Secret and server follow (accepted gap)
func (r *DBPasswordRotation) runOracle(ctx context.Context, env *Env, s *Secret, u *DBURL) error {
	name := r.Name()
	phase := s.Annotations[AnnPhase+name]
	if phase == "" {
		if r.o.OpenOracle == nil {
			return errors.New("oracle rotation not available in this build")
		}
		ora, err := r.o.OpenOracle(ctx, u.Raw)
		if err != nil {
			return err
		}
		defer func() { _ = ora.Close() }()
		pw, err := NewDBPassword()
		if err != nil {
			return err
		}
		if err := ora.ChangeOwnPassword(ctx, u.User, u.Password, pw); err != nil {
			return fmt.Errorf("oracle password change: %w", err)
		}
		if err := env.Transition(ctx, name, dbPhaseSwapped, func(s *Secret) {
			s.Data[r.o.URLKey] = u.WithCredentials(u.User, pw)
			for k, tmpl := range r.o.ExtraKeys {
				if tmpl == "password" {
					s.Data[k] = pw
				}
			}
		}); err != nil {
			return err
		}
		if s, err = env.Secrets.Get(ctx, r.o.DBSecret); err != nil {
			return err
		}
		phase = dbPhaseSwapped
	}
	if phase != dbPhaseSwapped {
		return fmt.Errorf("unknown %s phase %q", name, phase)
	}
	if err := env.WaitServerRolled(ctx, s, name); err != nil {
		return fmt.Errorf("oracle rollout: %w", err)
	}
	return env.Transition(ctx, name, "", nil)
}
```

- [ ] **Step 4: Run**

Run: `make test-unit name=TestDBPasswordRotation count1=true`

- [ ] **Step 5: Commit**

```bash
git add internal/rotator/db_password.go internal/rotator/db_password_test.go
git commit -m "feat(rotator): rotate the app database credential with alternating PostgreSQL users (#965)"
```

---
### Task 5: `DBAdminPasswordRotation`

**Files:**
- Create: `internal/rotator/db_admin_password.go`
- Test: `internal/rotator/db_admin_password_test.go`

**Interfaces:**
- Produces: `NewDBAdminPasswordRotation(open func(ctx, adminURL string) (DBAdmin, error)) *DBAdminPasswordRotation`, `Name() == "db-admin-password"`. Operates on Secret `tmi-rotator-admin` (`Env.SecretName` must be that Secret when this rotation runs: `cmd/rotator` runs it with a second `Env` whose `SecretName = AdminSecretName`; the phase annotations live on `tmi-rotator-admin`). No server rollout is involved; the Secret is referenced by nothing but the rotator.

Phases:

| phase | on entry | write | next |
|---|---|---|---|
| `""` | generate NEW | `TMI_DB_ADMIN_PENDING_PASSWORD = NEW` | `pending` |
| `pending` | open admin with current URL; if `Ping` fails, open with pending password (crash after ALTER) -> if that works, the change already happened; else `SetPassword(current_user, NEW, login=true)` | `TMI_DB_ADMIN_URL` = URL with NEW; pending deleted; rotated-at | done |

PostgreSQL only: the RDS master (or local `postgres`/`tmi_dev`) is never an Oracle user; on the Oracle overlay this rotation is not registered.

- [ ] **Step 1: Write the failing tests**

```go
package rotator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// openBy returns a fake admin whose Ping succeeds only for URLs carrying one of the accepted passwords.
func openBy(accepted map[string]*fakeDBAdmin) func(context.Context, string) (DBAdmin, error) {
	return func(_ context.Context, adminURL string) (DBAdmin, error) {
		u, err := ParseDBURL(adminURL)
		if err != nil {
			return nil, err
		}
		if a, ok := accepted[u.Password]; ok {
			return a, nil
		}
		return nil, errors.New("password authentication failed")
	}
}

func adminEnv(data map[string]string, ann map[string]string) (*Env, *MemorySecretStore) {
	env, _ := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{}, Annotations: map[string]string{}})
	st := NewMemorySecretStore(&Secret{Name: AdminSecretName, Data: data, Annotations: ann})
	env.Secrets = st
	env.Rollouts = NewFakeRolloutWaiter(st, "tmi-server")
	env.SecretName = AdminSecretName
	return env, st
}

func TestDBAdminRotation_FullCycle(t *testing.T) {
	cur := newFakeDBAdmin()
	env, st := adminEnv(map[string]string{AdminURLKey: "postgres://master:oldpw@db:5432/tmi?sslmode=require"}, map[string]string{})
	r := NewDBAdminPasswordRotation(openBy(map[string]*fakeDBAdmin{"oldpw": cur}))
	require.NoError(t, r.Run(context.Background(), env))
	s, _ := st.Get(context.Background(), AdminSecretName)
	u, _ := ParseDBURL(s.Data[AdminURLKey])
	require.Equal(t, "master", u.User)
	require.NotEqual(t, "oldpw", u.Password)
	require.Equal(t, u.Password, cur.passwords["master"])
	_, pending := s.Data[AdminPendingKey]
	require.False(t, pending)
	require.Empty(t, s.Annotations[AnnPhase+"db-admin-password"])
	require.NotEmpty(t, s.Annotations[AnnRotatedAt+"db-admin-password"])
}

func TestDBAdminRotation_ResumePendingEitherWay(t *testing.T) {
	// Case A: crashed before ALTER: current still works, pending does not.
	cur := newFakeDBAdmin()
	env, st := adminEnv(map[string]string{AdminURLKey: "postgres://master:oldpw@db:5432/tmi", AdminPendingKey: "NewPw1x"},
		map[string]string{AnnPhase + "db-admin-password": "pending"})
	r := NewDBAdminPasswordRotation(openBy(map[string]*fakeDBAdmin{"oldpw": cur}))
	require.NoError(t, r.Run(context.Background(), env))
	s, _ := st.Get(context.Background(), AdminSecretName)
	u, _ := ParseDBURL(s.Data[AdminURLKey])
	require.Equal(t, "NewPw1x", u.Password)
	require.Equal(t, "NewPw1x", cur.passwords["master"])

	// Case B: crashed after ALTER: only pending works; no second ALTER.
	after := newFakeDBAdmin()
	env, st = adminEnv(map[string]string{AdminURLKey: "postgres://master:oldpw@db:5432/tmi", AdminPendingKey: "NewPw1x"},
		map[string]string{AnnPhase + "db-admin-password": "pending"})
	r = NewDBAdminPasswordRotation(openBy(map[string]*fakeDBAdmin{"NewPw1x": after}))
	require.NoError(t, r.Run(context.Background(), env))
	s, _ = st.Get(context.Background(), AdminSecretName)
	u, _ = ParseDBURL(s.Data[AdminURLKey])
	require.Equal(t, "NewPw1x", u.Password)
	require.Empty(t, after.passwords, "no ALTER when the pending password already authenticates")
}

func TestDBAdminRotation_NeitherPasswordWorks(t *testing.T) {
	env, st := adminEnv(map[string]string{AdminURLKey: "postgres://master:oldpw@db:5432/tmi", AdminPendingKey: "NewPw1x"},
		map[string]string{AnnPhase + "db-admin-password": "pending"})
	r := NewDBAdminPasswordRotation(openBy(map[string]*fakeDBAdmin{}))
	require.Error(t, r.Run(context.Background(), env))
	s, _ := st.Get(context.Background(), AdminSecretName)
	require.Equal(t, "pending", s.Annotations[AnnPhase+"db-admin-password"], "state kept for the runbook")
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestDBAdminRotation count1=true`
Expected: undefined.

- [ ] **Step 3: Implement `db_admin_password.go`**

```go
package rotator

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	dbAdminRotationName = "db-admin-password"
	dbAdminPhasePending = "pending"
)

// DBAdminPasswordRotation rotates the credential the rotator itself uses.
// SEM@<sha>: crash-safe rotation of the administrative database password via a pending value
type DBAdminPasswordRotation struct {
	open func(ctx context.Context, adminURL string) (DBAdmin, error)
}

// SEM@<sha>: build a DBAdminPasswordRotation (pure)
func NewDBAdminPasswordRotation(open func(ctx context.Context, adminURL string) (DBAdmin, error)) *DBAdminPasswordRotation {
	return &DBAdminPasswordRotation{open: open}
}

// SEM@<sha>: return the rotation name (pure)
func (r *DBAdminPasswordRotation) Name() string { return dbAdminRotationName }

// SEM@<sha>: advance the admin password rotation: record pending, change it, then commit the URL
func (r *DBAdminPasswordRotation) Run(ctx context.Context, env *Env) error {
	logger := slogging.Get()
	if env.SecretName != AdminSecretName {
		return fmt.Errorf("db-admin-password rotation needs Env.SecretName == %q", AdminSecretName)
	}
	s, err := env.Secrets.Get(ctx, AdminSecretName)
	if err != nil {
		return err
	}
	name := r.Name()
	phase := s.Annotations[AnnPhase+name]
	if phase == "" {
		pw, err := NewDBPassword()
		if err != nil {
			return err
		}
		if err := env.Transition(ctx, name, dbAdminPhasePending, func(s *Secret) { s.Data[AdminPendingKey] = pw }); err != nil {
			return err
		}
		if s, err = env.Secrets.Get(ctx, AdminSecretName); err != nil {
			return err
		}
		phase = dbAdminPhasePending
	}
	if phase != dbAdminPhasePending {
		return fmt.Errorf("unknown %s phase %q", name, phase)
	}
	u, err := ParseDBURL(s.Data[AdminURLKey])
	if err != nil {
		return fmt.Errorf("%s: %w", AdminURLKey, err)
	}
	pending := s.Data[AdminPendingKey]
	if pending == "" {
		return fmt.Errorf("%s missing in phase pending", AdminPendingKey)
	}
	newURL := u.WithCredentials(u.User, pending)

	admin, err := r.open(ctx, u.Raw)
	switch {
	case err == nil:
		defer func() { _ = admin.Close() }()
		if err := admin.SetPassword(ctx, u.User, pending, true); err != nil {
			return fmt.Errorf("change admin password: %w", err)
		}
		logger.Info("Admin database password changed user=%s", u.User)
	default:
		logger.Warn("Admin URL no longer authenticates; trying the pending password (resume after a crash)")
		adminNew, err2 := r.open(ctx, newURL)
		if err2 != nil {
			return fmt.Errorf("neither the current nor the pending admin password authenticates (see runbook: RDS master recovery): current=%v pending=%v", err, err2)
		}
		_ = adminNew.Close()
		logger.Info("Pending admin password already active user=%s", u.User)
	}
	return env.Transition(ctx, name, "", func(s *Secret) {
		s.Data[AdminURLKey] = newURL
		delete(s.Data, AdminPendingKey)
	})
}
```

- [ ] **Step 4: Run**

Run: `make test-unit name=TestDBAdminRotation count1=true`

- [ ] **Step 5: Commit**

```bash
git add internal/rotator/db_admin_password.go internal/rotator/db_admin_password_test.go
git commit -m "feat(rotator): crash-safe rotation of the admin database credential (#965)"
```

---

### Task 6: Wiring: `cmd/rotator`, Secrets, overlays, Terraform, deploy scripts

**Files:**
- Modify: `cmd/rotator/main.go` (`run()`), `internal/config/process_env.go` (+ `make generate-config-docs`), `scripts/rotate-secret.py` (`VALID` += `db-password`, `db-admin-password`)
- Modify: `deployments/k8s/dev/rotator.yml` (env for the DB Secret names; Oracle wallet mount is overlay-only)
- Create: `deployments/k8s/dev/docker-desktop-oracle/patches/rotator-oracle.yaml`; Modify: `deployments/k8s/dev/docker-desktop-oracle/kustomization.yaml`
- Modify: `scripts/lib/deploy.py` (`ensure_rotator_admin_secret()`), `scripts/deploy-aws.sh` (`import_config`)
- Modify: `terraform/modules/kubernetes/aws/k8s_resources.tf` (new `kubernetes_secret_v1.rotator_admin`), `terraform/modules/database/aws/main.tf:45-75` (`lifecycle { ignore_changes = [password] }`), `terraform/modules/secrets/aws/{main,outputs}.tf` (drop `db_credentials` and the module's Secrets Manager use entirely; keep `random_password.db_password` as the RDS seed), `terraform/modules/kubernetes/aws/main.tf:685-700` + `variables.tf:86` (drop `secret_arns` and the `secretsmanager:GetSecretValue` statement; keep the `tmi-api` SA and role), `terraform/environments/aws-public/main.tf` (`secret_arns` line, `import_config` comment)

- [ ] **Step 1: `cmd/rotator/main.go`**

Add to `options`: `DBSecret` (`TMI_ROTATOR_DB_SECRET`, default `tmi-secrets`), `DBURLKey` (`TMI_ROTATOR_DB_URL_KEY`, default `TMI_DATABASE_URL`), `DBExtraPasswordKey` (`TMI_ROTATOR_DB_PASSWORD_KEY`, default empty; Oracle overlay sets `oracle-password`), `PoolDrain` (`TMI_ROTATOR_DB_POOL_DRAIN`, default `5m`), `AdminEnabled` derived: register `db-admin-password` only when `tmi-rotator-admin` exists and the app URL is PostgreSQL. In `run()`:

```go
	dbOpts := rotator.DBPasswordOptions{
		DBSecret: o.DBSecret, URLKey: o.DBURLKey,
		OpenPostgres: func(ctx context.Context, url string) (rotator.DBAdmin, error) { return rotator.OpenPostgresAdmin(ctx, url) },
		OpenOracle:   func(ctx context.Context, url string) (rotator.OracleUserAdmin, error) { return rotator.OpenOracleUserAdmin(ctx, url, o.OracleWallet) },
		PoolDrain:    o.PoolDrain,
	}
	if o.DBExtraPasswordKey != "" {
		dbOpts.ExtraKeys = map[string]string{o.DBExtraPasswordKey: "password"}
	}
	var firstErr error // PR 1's single rotator.Run call becomes: if err := rotator.Run(...); err != nil { firstErr = err }
	appURL, _ := rotator.ParseDBURL(dbURL)
	appURLIsPostgres := appURL != nil && appURL.IsPostgres()
	dbEnv := *env
	dbEnv.SecretName = o.DBSecret
	if err := rotator.Run(ctx, &dbEnv, []rotator.Rotation{rotator.NewDBPasswordRotation(dbOpts)}, o.Force); err != nil {
		firstErr = errors.Join(firstErr, err)
	}
	if _, err := secrets.Get(ctx, rotator.AdminSecretName); err == nil && appURLIsPostgres {
		adminEnv := *env
		adminEnv.SecretName = rotator.AdminSecretName
		if err := rotator.Run(ctx, &adminEnv, []rotator.Rotation{rotator.NewDBAdminPasswordRotation(func(ctx context.Context, url string) (rotator.DBAdmin, error) { return rotator.OpenPostgresAdmin(ctx, url) })}, o.Force); err != nil {
			firstErr = errors.Join(firstErr, err)
		}
	}
```

Order in `run()`: JWT keyring (PR 2) and the PR 1 rotations first (they need the DB connection the rotator opened with the *current* app URL), then `db-password`, then `db-admin-password`. The rotator's own GORM connection for the settings-key re-encrypt uses the app URL read at start; a DB rotation in the same run happens after it and never revokes the master, so the earlier connection stays valid. Exit non-zero if any `Run` failed.

`process_env.go`: entries for the four new vars (Binary `rotator`) plus the two Secret data keys `TMI_DB_ADMIN_URL` and `TMI_DB_ADMIN_PENDING_PASSWORD` (`Secret: true`, Purpose "Secret data key in tmi-rotator-admin written by tmi-rotator; never read from the environment"), because `TestRepoTMIEnvTokens_AreAllDocumented` scans every non-test `.go` file for `TMI_*` literals. Then `make generate-config-docs` and `make test-unit name=TestRepoTMIEnvTokens count1=true`.

- [ ] **Step 2: Manifests**

`deployments/k8s/dev/rotator.yml` env: add `- { name: TMI_ROTATOR_DB_SECRET, value: "tmi-secrets" }` and `- { name: TMI_ROTATOR_DB_URL_KEY, value: "TMI_DATABASE_URL" }` (explicit defaults so the Oracle patch has something to replace); `Role.rules` already lists `tmi-rotator-admin`. `docker-desktop-oracle/patches/rotator-oracle.yaml` (strategic merge on the CronJob): env `TMI_ROTATOR_DB_SECRET=tmi-oracle-db`, `TMI_ROTATOR_DB_URL_KEY=database-url`, `TMI_ROTATOR_DB_PASSWORD_KEY=oracle-password`, `TMI_ORACLE_WALLET_LOCATION=/etc/tmi-oracle-wallet`, and `TMI_DATABASE_URL` as a `secretKeyRef` to `tmi-oracle-db/database-url` (the PR 1 `run()` opens its GORM connection from `o.DatabaseURL` when set; `tmi-secrets` has no URL on the Oracle overlay), plus the wallet volume/mount copied from `server-oracle.yml`; and the Role gets `tmi-oracle-db` in `resourceNames` (a second patch on the Role, or a JSON patch). The PR 1/PR 2 rotations keep `TMI_ROTATOR_SECRET=tmi-secrets`; only the DB rotation gets its own Secret through `dbEnv` in `run()`.

Also add the server label selector for the k3s Postgres policy is already done in PR 1; nothing new. `kubectl kustomize deployments/k8s/dev/docker-desktop-oracle | kubectl apply --dry-run=client -f -` must parse.

- [ ] **Step 3: `scripts/lib/deploy.py`**

```python
def ensure_rotator_admin_secret(cluster_target: str, db: str) -> None:
    """Create Secret/tmi-rotator-admin on a dev cluster (#965 PR 3) from the
    in-cluster Postgres superuser (postgres.yml: tmi_dev/dev123). Idempotent;
    the rotator rewrites it after the first admin rotation. Oracle dev has no
    admin credential (single-user rotation)."""
    if db == "oracle":
        return
    if kubectl(["-n", NS, "get", "secret", "tmi-rotator-admin"], check=False, capture=True).returncode == 0:
        return
    url = f"postgres://tmi_dev:dev123@{in_cluster_db_host(cluster_target)}:5432/tmi_dev?sslmode=disable"
    with tempfile.TemporaryDirectory() as tmp:
        f = Path(tmp) / "TMI_DB_ADMIN_URL"
        f.write_text(url)
        os.chmod(f, 0o600)
        kubectl(["-n", NS, "create", "secret", "generic", "tmi-rotator-admin", f"--from-file=TMI_DB_ADMIN_URL={f}"], capture=True)
    log_success("Secret/tmi-rotator-admin created (dev superuser; rotated by tmi-rotator)")
```

Call it in `start()` after `ensure_settings_key_seeded()`. The dev password `dev123` is committed in `postgres.yml` already; this is not a new disclosure. Keep `tmi-rotator-admin` on `dev-down` (same reason as `tmi-secrets`); `dev-nuke` removes the namespace.

- [ ] **Step 4: Terraform**

`k8s_resources.tf`:

```hcl
# Rotator-only admin credential (#965 PR 3): the RDS master URL. Seeded once,
# then rotated by tmi-rotator; never referenced by a workload, so a change
# never rolls anything. The RDS resource ignores password drift for the same
# reason (terraform/modules/database/aws/main.tf).
resource "kubernetes_secret_v1" "rotator_admin" {
  metadata {
    name      = "tmi-rotator-admin"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
  }
  data = {
    TMI_DB_ADMIN_URL = "postgresql://${var.db_username}:${urlencode(var.db_password)}@${var.db_host}:${var.db_port}/${var.db_name}?sslmode=require"
  }
  lifecycle {
    ignore_changes = [data, metadata[0].annotations]
  }
}
```

`database/aws/main.tf` `aws_db_instance.tmi`: add `lifecycle { ignore_changes = [password] }` with a comment naming the runbook (`aws rds modify-db-instance --master-user-password` if both stored passwords are lost) and stating that `log_statement` stays at the engine default (`none`) so `ALTER ROLE ... PASSWORD` is never logged; add a `parameter { name = "log_statement" value = "none" }` to the parameter group to pin it. Secrets module: delete `aws_secretsmanager_secret.db_credentials` and its version and outputs (`db_credentials_secret_arn/_name`, `secrets_provider_id`, `secret_arns`); the module now only generates `random_password.db_password`, `random_password.redis_password`, `random_id.settings_encryption_key` (rename the module comment). Kubernetes module: delete `variable "secret_arns"` and the IAM policy statement that grants `secretsmanager:GetSecretValue`; keep `aws_iam_role.tmi_pod` and the `tmi-api` SA (IRSA stays for future AWS access; update the SA comment). `aws-public/main.tf`: delete `secret_arns = values(module.secrets.secret_arns)`. `AWS_PROFILE=tmi terraform validate`; read `terraform plan`: one Secrets Manager secret destroyed, one Secret created, IAM policy updated, no RDS change.

- [ ] **Step 5: `scripts/deploy-aws.sh import_config`**

Replace the Secrets Manager DB credential fetch: the URL comes from `tmi-secrets/TMI_DATABASE_URL` (the app user, `tmi_a`/`tmi_b`, which owns the schema via `tmi_owner`; importing as it is correct because rows land under the owner role). Write it to a `umask 077` file and reference it from the transient config:

```bash
    kubectl -n "${NAMESPACE}" get secret tmi-secrets \
        -o 'go-template={{index .data "TMI_DATABASE_URL" | base64decode}}' > "${tmp_dir}/database-url"
    # The in-cluster host is unreachable from the laptop; swap host:port for the socat proxy.
    sed -i.bak -E "s#@[^/]+/#@localhost:15432/#" "${tmp_dir}/database-url" && rm -f "${tmp_dir}/database-url.bak"
```

and in the YAML `database: url: "file://${tmp_dir}/database-url"` (config supports `file://` references for `database.url`: `internal/config/config.go:1051`; confirm `dbtool` calls the same resolver, `rg -n "ResolveSecretReferences|resolveSecretReferences" cmd/dbtool`; if it does not, add the call in `cmd/dbtool/main.go` right after config load). Delete the `aws secretsmanager get-secret-value` block, `urlencode()` if now unused, and the `db_user`/`db_password` variables. `bash -n scripts/deploy-aws.sh`.

- [ ] **Step 6: Commit**

```bash
git add cmd/rotator internal/config/process_env.go config-reference.md scripts/rotate-secret.py deployments/k8s scripts/lib/deploy.py scripts/deploy-aws.sh terraform
git commit -m "feat(deploy): wire database credential rotation, admin Secret and RDS password hand-off (#965)"
```

---

### Task 7: Integration tests (PostgreSQL and Oracle)

**Files:**
- Create: `test/integration/workflows/db_rotation_test.go`

The harness server runs in Docker as the harness superuser (`TEST_DB_USER`, default `tmi_dev`, database `tmi_test`); the rotator runs in-process with a `MemorySecretStore`, so the server keeps its (never-revoked) superuser connection and the API loop must stay clean. What the test proves on PostgreSQL: bootstrap is idempotent and moves ownership; `tmi_a` can connect and create objects that land under `tmi_owner`; rotation alternates users and revokes the old one; the API stays 2xx throughout. The real switch of the server to `tmi_a` is verified on k3s-rp (Task 9).

- [ ] **Step 1: Write the tests**

```go
package workflows

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ericfitz/tmi/internal/rotator"
	"github.com/ericfitz/tmi/test/integration/framework"
)

func harnessAdminURL(t *testing.T) string {
	t.Helper()
	host := os.Getenv("TEST_DB_HOST")
	port := os.Getenv("TEST_DB_PORT")
	user := os.Getenv("TEST_DB_USER")
	pw := os.Getenv("TEST_DB_PASSWORD")
	name := os.Getenv("TEST_DB_NAME")
	if host == "" || user == "" {
		t.Skip("harness DB env not set")
	}
	return "postgres://" + user + ":" + pw + "@" + host + ":" + port + "/" + name + "?sslmode=disable"
}

func ownerOf(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var owner string
	err := db.QueryRow(`SELECT pg_get_userbyid(c.relowner) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname=$1`, table).Scan(&owner)
	framework.AssertNoError(t, err, "owner query")
	return owner
}

func TestDBRotationIntegration_BootstrapOwnership(t *testing.T) {
	if os.Getenv("TMI_DATABASE_URL") != "" && len(os.Getenv("TMI_DATABASE_URL")) > 9 && os.Getenv("TMI_DATABASE_URL")[:9] == "oracle://" {
		t.Skip("PostgreSQL only")
	}
	adminURL := harnessAdminURL(t)
	ctx := context.Background()
	admin, err := rotator.OpenPostgresAdmin(ctx, adminURL)
	framework.AssertNoError(t, err, "admin")
	defer admin.Close()
	appUser := os.Getenv("TEST_DB_USER")

	pwA, err := admin.Bootstrap(ctx, appUser)
	framework.AssertNoError(t, err, "bootstrap")
	_, err = admin.Bootstrap(ctx, appUser) // idempotent
	framework.AssertNoError(t, err, "bootstrap again")

	sqlDB, _ := sql.Open("pgx", adminURL)
	defer sqlDB.Close()
	for _, tbl := range []string{"users", "system_settings", "threat_models"} {
		if got := ownerOf(t, sqlDB, tbl); got != rotator.PGOwnerRole {
			t.Fatalf("%s owned by %s, want %s", tbl, got, rotator.PGOwnerRole)
		}
	}
	var seqOwner string
	framework.AssertNoError(t, sqlDB.QueryRow(`SELECT pg_get_userbyid(relowner) FROM pg_class WHERE relname='tmi_threat_model_alias_seq'`).Scan(&seqOwner), "seq owner")
	if seqOwner != rotator.PGOwnerRole {
		t.Fatalf("sequence owned by %s", seqOwner)
	}

	// tmi_a connects, and what it creates is owned by tmi_owner (SET role).
	u, _ := rotator.ParseDBURL(adminURL)
	aURL := u.WithCredentials(rotator.PGUserA, pwA)
	aDB, err := sql.Open("pgx", aURL)
	framework.AssertNoError(t, err, "open as tmi_a")
	defer aDB.Close()
	_, err = aDB.Exec(`CREATE TABLE IF NOT EXISTS tmi_rotation_probe (id int)`)
	framework.AssertNoError(t, err, "create as tmi_a")
	if got := ownerOf(t, sqlDB, "tmi_rotation_probe"); got != rotator.PGOwnerRole {
		t.Fatalf("probe table owned by %s", got)
	}
	_, _ = sqlDB.Exec(`DROP TABLE tmi_rotation_probe`)
	var n int
	framework.AssertNoError(t, aDB.QueryRow(`SELECT count(*) FROM system_settings`).Scan(&n), "tmi_a reads app tables")
}

func TestDBRotationIntegration_AlternateUnderLoad(t *testing.T) {
	adminURL := harnessAdminURL(t)
	serverURL := os.Getenv("TMI_SERVER_URL")
	tokens, err := framework.AuthenticateUser("alice")
	framework.AssertNoError(t, err, "auth")
	client, _ := framework.NewClient(serverURL, tokens)

	ctx := context.Background()
	admin, _ := rotator.OpenPostgresAdmin(ctx, adminURL)
	pwA, err := admin.Bootstrap(ctx, os.Getenv("TEST_DB_USER"))
	framework.AssertNoError(t, err, "bootstrap")
	admin.Close()
	u, _ := rotator.ParseDBURL(adminURL)

	st := rotator.NewMemorySecretStore(
		&rotator.Secret{Name: "tmi-secrets", Data: map[string]string{"TMI_DATABASE_URL": u.WithCredentials(rotator.PGUserA, pwA)}, Annotations: map[string]string{}},
		&rotator.Secret{Name: rotator.AdminSecretName, Data: map[string]string{rotator.AdminURLKey: adminURL}, Annotations: map[string]string{}},
	)
	env := &rotator.Env{Secrets: st, Rollouts: rotator.NewFakeRolloutWaiter(st, "tmi-server"), SecretName: "tmi-secrets", ServerDeployment: "tmi-server", RolloutTimeout: time.Second, Now: time.Now}
	r := rotator.NewDBPasswordRotation(rotator.DBPasswordOptions{
		OpenPostgres: func(ctx context.Context, url string) (rotator.DBAdmin, error) { return rotator.OpenPostgresAdmin(ctx, url) },
		PoolDrain:    2 * time.Second,
	})
	stop := make(chan struct{})
	bad := apiLoop(t, client, stop) // from secret_rotation_test.go (PR 1)
	err = r.Run(ctx, env)
	close(stop)
	framework.AssertNoError(t, err, "rotation")
	if n := atomic.LoadInt32(bad); n != 0 {
		t.Fatalf("%d failed API calls during DB rotation", n)
	}
	s, _ := st.Get(ctx, "tmi-secrets")
	nu, _ := rotator.ParseDBURL(s.Data["TMI_DATABASE_URL"])
	if nu.User != rotator.PGUserB {
		t.Fatalf("expected tmi_b, got %s", nu.User)
	}
	bDB, _ := sql.Open("pgx", s.Data["TMI_DATABASE_URL"])
	framework.AssertNoError(t, bDB.Ping(), "tmi_b connects")
	bDB.Close()
	aDB, _ := sql.Open("pgx", u.WithCredentials(rotator.PGUserA, pwA))
	if err := aDB.Ping(); err == nil {
		t.Fatal("tmi_a still logs in after rotation")
	}
	aDB.Close()

	// Admin rotation, then the new admin URL still works.
	adminEnv := *env
	adminEnv.SecretName = rotator.AdminSecretName
	ar := rotator.NewDBAdminPasswordRotation(func(ctx context.Context, url string) (rotator.DBAdmin, error) { return rotator.OpenPostgresAdmin(ctx, url) })
	framework.AssertNoError(t, ar.Run(ctx, &adminEnv), "admin rotation")
	as, _ := st.Get(ctx, rotator.AdminSecretName)
	a2, err := rotator.OpenPostgresAdmin(ctx, as.Data[rotator.AdminURLKey])
	framework.AssertNoError(t, err, "new admin URL")
	// Restore the harness superuser password so later phases keep working.
	framework.AssertNoError(t, a2.SetPassword(ctx, u.User, u.Password, true), "restore")
	a2.Close()
	resp, err := client.Do(framework.Request{Method: http.MethodGet, Path: "/me"})
	framework.AssertNoError(t, err, "after")
	framework.AssertStatusOK(t, resp)
}

// Oracle: the app user changes its own password; the harness server (which
// keeps its pooled connections) must recover once the URL is updated. Runs
// only under make test-integration-oci (name contains OracleIntegration).
func TestDBRotationOracleIntegration_SelfChangeAndRecover(t *testing.T) {
	url := os.Getenv("TMI_DATABASE_URL")
	if len(url) < 9 || url[:9] != "oracle://" {
		t.Skip("Oracle only")
	}
	ctx := context.Background()
	st := rotator.NewMemorySecretStore(&rotator.Secret{Name: "tmi-oracle-db", Data: map[string]string{"database-url": url}, Annotations: map[string]string{}})
	env := &rotator.Env{Secrets: st, Rollouts: rotator.NewFakeRolloutWaiter(st, "tmi-server"), SecretName: "tmi-oracle-db", ServerDeployment: "tmi-server", RolloutTimeout: time.Second, Now: time.Now}
	r := rotator.NewDBPasswordRotation(rotator.DBPasswordOptions{DBSecret: "tmi-oracle-db", URLKey: "database-url",
		OpenOracle: func(ctx context.Context, u string) (rotator.OracleUserAdmin, error) { return rotator.OpenOracleUserAdmin(ctx, u, os.Getenv("TMI_ORACLE_WALLET_LOCATION")) }})
	framework.AssertNoError(t, r.Run(ctx, env), "oracle rotation")
	s, _ := st.Get(ctx, "tmi-oracle-db")
	nu, _ := rotator.ParseDBURL(s.Data["database-url"])
	ora, err := rotator.OpenOracleUserAdmin(ctx, s.Data["database-url"], os.Getenv("TMI_ORACLE_WALLET_LOCATION"))
	framework.AssertNoError(t, err, "new password works")
	// Put the original password back so the rest of the OCI suite is unaffected.
	ou, _ := rotator.ParseDBURL(url)
	framework.AssertNoError(t, ora.ChangeOwnPassword(ctx, nu.User, nu.Password, ou.Password), "restore")
	ora.Close()
}
```

The Oracle test file needs the `oracle` build tag only for `OpenOracleUserAdmin` to be real; under `!oracle` it returns an error and the test is skipped by the URL check anyway.

- [ ] **Step 2: Run**

`cd test/integration && go mod tidy`; `make test-integration` (PostgreSQL); `make test-integration-oci` (Oracle, needs `scripts/oci-env.sh` and a started ADB). Expected: the PostgreSQL tests execute and pass; the Oracle test passes on OCI and skips on PostgreSQL.

- [ ] **Step 3: Commit**

```bash
git add test/integration
git commit -m "test(integration): PostgreSQL role bootstrap, alternating-user rotation and Oracle self-change (#965)"
```

---

### Task 8: Oracle compatibility review

- [ ] **Step 1:** Invoke the `oracle-db-admin` skill on the whole PR diff, pointing it at: the ownership transfer (sequences `tmi_threat_model_alias_seq`, the audit trigger function `tmi_audit_append_only_guard`, PostgreSQL only), the `SET role` approach versus `ALTER DEFAULT PRIVILEGES`, GORM `AutoMigrate` behaviour under a `SET role` session (any hook that reads `current_user`?), `ALTER USER ... IDENTIFIED BY ... REPLACE` semantics on ADB (password policy, `PASSWORD_REUSE_TIME` profile limits that could reject a rotation), and the accepted single-user gap.
- [ ] **Step 2:** Address the verdict (`APPROVED` / `APPROVED WITH NOTES` / `BLOCKING ISSUES`) as project policy requires; commit fixes as `fix(rotator): Oracle review follow-ups (#965)`.

---

### Task 9: Cluster verification, runbook, PR, closing #965

- [ ] **Step 1: docker-desktop.** `make dev-up` (creates `tmi-rotator-admin`), `make rotate-secret name=db-password`: expected log `Database role model bootstrapped; app moves from tmi_dev to tmi_a`, one server roll, `curl localhost:8080/` 200, `make test-integration` green while the server runs as `tmi_a`. Run it again: `Database idle user enabled user=tmi_b`, roll, drain, `Database user retired user=tmi_a`. `kubectl -n tmi-platform exec statefulset/postgres -- psql -U tmi_dev -c "\du"` shows `tmi_a` NOLOGIN, `tmi_b` LOGIN, both members of `tmi_owner`.
- [ ] **Step 2: Admin rotation.** `make rotate-secret name=db-admin-password`; then `kubectl -n tmi-platform get secret tmi-rotator-admin -o jsonpath='{.metadata.annotations}'` shows `rotated-at.db-admin-password`; a forced `db-password` afterwards still works (proves the new admin URL).
- [ ] **Step 3: k3s-rp.** Same sequence plus the full integration suite and a CATS smoke (`make cats-fuzz` per the `cats-tmi` skill) against the rotated cluster: zero 5xx.
- [ ] **Step 4: Oracle dev.** `DB=oracle make dev-up`, `make rotate-secret name=db-password`: expected the ADB password changes, the server rolls, a few requests in the gap may fail (accepted), then healthy.
- [ ] **Step 5: Runbook.** Extend the wiki `Secret-Rotation.md` (PR 1) with: the role model (`tmi_owner`, `tmi_a`/`tmi_b`), which user is live (`TMI_DATABASE_URL`), recovering a stuck `swapped` phase (both users have LOGIN; safe to re-run), recovering `db-admin-password` in `pending` when neither password works (`aws rds modify-db-instance --db-instance-identifier tmi-postgres --master-user-password ...` from a `umask 077` file, then patch `tmi-rotator-admin`), Oracle single-user behaviour, and `import_config` now using the app URL. Update `Database-Operations.md` (the app no longer runs as the master).
- [ ] **Step 6: Gates.** `make lint`, `make build-server`, `make build-rotator`, `make test-unit`, `make test-integration`, `make test-integration-oci`, `security-review` skill.
- [ ] **Step 7: PR.** Push; `gh pr create --title "feat(rotator): rotate database credentials with alternating PostgreSQL users (#965)"`, body: role model, phase tables, Oracle verdict, cluster results, Open questions, and `Closes #965` (this is the last of the three PRs; the issue is closed by the squash-merge to `main`). Attribution trailer. After merge: the single AWS deploy that the spec schedules after PRs 1-3, #968 and #972 hands `tmi-secrets` to the rotator and bootstraps DB users and the JWT keyring on its first run (`deploy-aws.sh bootstrap_secrets`, PR 2).

## Self-review notes (writer)

- Spec §4 coverage: bootstrap (Tasks 2, 4, 7), rotation (Task 4), admin credential with pending password (Task 5), Terraform `ignore_changes = [password]` and runbook (Tasks 6, 9), Oracle path (Tasks 3, 4, 7), Oracle review (Task 8).
- Review Focus 1 (`TestPostgresBootstrap_Idempotent`) is realised as `TestDBRotationIntegration_BootstrapOwnership` in Task 7 (needs a live PostgreSQL; the statement-level plan is unit-tested in Task 2).
