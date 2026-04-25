package api

// toWireDiagnostics converts the internal diagnostics builder type to the
// generated OpenAPI wire type. Returns nil when the input is nil.
func toWireDiagnostics(d *AccessDiagnosticsDiag) *DocumentAccessDiagnostics {
	if d == nil {
		return nil
	}
	out := &DocumentAccessDiagnostics{
		ReasonCode:   DocumentAccessDiagnosticsReasonCode(d.ReasonCode),
		ReasonDetail: d.ReasonDetail,
		Remediations: make([]AccessRemediation, 0, len(d.Remediations)),
	}
	for _, r := range d.Remediations {
		out.Remediations = append(out.Remediations, AccessRemediation{
			Action: AccessRemediationAction(r.Action),
			Params: r.Params,
		})
	}
	return out
}
