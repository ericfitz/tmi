package api

import "testing"

// #885: the event-handler pattern must need tag context, not just `on\w+=`.
func TestCheckHTMLInjection_EventHandlerNeedsTagContext(t *testing.T) {
	for _, ok := range []string{
		"Set deletion_protection = true on the bucket",
		"version = 2",
		"configuration=prod; only = one",
	} {
		if err := CheckHTMLInjection(ok, "mitigation"); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{
		`<img src=x onerror=alert(1)>`,
		`<IMG/ONERROR = alert(1)>`,
		"<div\nonclick=x>",
		`<a href="#" onmouseover='x'>`,
	} {
		if err := CheckHTMLInjection(bad, "mitigation"); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
