//go:build !oracle

package framework

import "errors"

// newOracleDevDatabase is the stub for builds without the oracle tag: the
// godror driver needs CGO and the Oracle Instant Client, so only the OCI
// runner compiles it in (scripts/run-integration-tests.py --target oci) (#898).
func newOracleDevDatabase(string) (*TestDatabase, error) {
	return nil, errors.New("TMI_DATABASE_URL is oracle:// but the test binary was built without -tags oracle")
}
