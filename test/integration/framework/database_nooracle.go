//go:build !oracle

package framework

import "errors"

// newOracleDevDatabase is the stub for builds without the oracle tag: the
// godror driver needs CGO and the Oracle Instant Client, so only the OCI
// runner compiles it in (scripts/run-integration-tests.py --target oci) (#898).
// SEM@b01ccb8e475aed5b956de76b96fe25b3de6076d0: reject opening an Oracle test database on a non-oracle build (pure)
func newOracleDevDatabase(string) (*TestDatabase, error) {
	return nil, errors.New("TMI_DATABASE_URL is oracle:// but the test binary was built without -tags oracle")
}
