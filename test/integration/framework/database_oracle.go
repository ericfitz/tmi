//go:build oracle

package framework

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/godror/godror"
)

// newOracleDevDatabase opens the Oracle database behind TMI_DATABASE_URL for
// the direct-DB test helpers. Same URL grammar as the server (auth/db/gorm.go
// parseOracleURL): oracle://user@tns_alias with the wallet resolved through
// TNS_ADMIN, or oracle://user:pass@host:port/service; the password comes from
// the URL or ORACLE_PASSWORD, exactly as scripts/oci-env.sh exports it (#898).
func newOracleDevDatabase(rawURL string) (*TestDatabase, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse TMI_DATABASE_URL: %w", err)
	}
	var params godror.ConnectionParams
	if u.User != nil {
		params.Username = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			params.Password = godror.NewPassword(pw)
		}
	}
	if params.Password.IsZero() {
		params.Password = godror.NewPassword(os.Getenv("ORACLE_PASSWORD"))
	}
	if u.Port() == "" && u.Path == "" {
		params.ConnectString = u.Hostname() // TNS alias from the wallet's tnsnames.ora
	} else {
		port := u.Port()
		if port == "" {
			port = "1521"
		}
		params.ConnectString = fmt.Sprintf("%s:%s%s", u.Hostname(), port, u.Path)
	}
	// Same UTC session basis as the server's connector (auth/db/gorm_oracle.go,
	// #459): pooled ADB sessions otherwise sit at the client host's local zone,
	// which shifts every naked TIMESTAMP literal the tests compare against.
	params.Timezone = time.UTC
	params.OnInitStmts = []string{"ALTER SESSION SET TIME_ZONE = '+00:00'"}
	params.InitOnNewConn = false
	sqlDB := sql.OpenDB(godror.NewConnector(params))
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("failed to ping Oracle dev database: %w", err)
	}
	return &TestDatabase{db: sqlDB, dialect: "oracle"}, nil
}
