package tier3_edge_cases

import (
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION_TESTS") == "true" {
		if err := framework.EnsureOAuthStubRunning(); err == nil {
			_ = framework.ClearRateLimits()
		}
	}
	os.Exit(m.Run())
}
