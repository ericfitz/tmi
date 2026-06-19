//go:build !oracle

package dberrors

// classifyOracleError is a no-op when built without the oracle tag.
// The oracle build tag pulls in godror which requires CGO and Oracle Instant Client.
// SEM@6a279d3dfc40bdd9ee0faa2abb1456f6dc5e003b: no-op Oracle error classifier for non-Oracle builds; always returns nil (pure)
func classifyOracleError(_ error) error {
	return nil
}
