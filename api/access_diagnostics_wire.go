package api

// toWireDiagnostics converts the internal diagnostics builder type to the
// generated OpenAPI wire type. Returns nil when the input is nil.
// SEM@5fe247aef5f2eedfc42d4adf9058c24de12eb56e: convert internal access diagnostics to the OpenAPI wire type (pure)
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
