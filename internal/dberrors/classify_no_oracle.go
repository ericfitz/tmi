//go:build !oracle

package dberrors

// classifyOracleError is a no-op when built without the oracle tag.
// The oracle build tag pulls in godror which requires CGO and Oracle Instant Client.
func classifyOracleError(_ error) error {
	return nil
}
