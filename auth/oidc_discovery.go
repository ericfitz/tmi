package auth

// OIDCDiscoveryDoc represents the subset of an OpenID Connect Discovery 1.0
// metadata document we need to classify an OAuth provider. Field names match
// the spec; only the fields we consume are declared.
type OIDCDiscoveryDoc struct {
	Issuer                 string   `json:"issuer"`
	AuthorizationEndpoint  string   `json:"authorization_endpoint"`
	TokenEndpoint          string   `json:"token_endpoint"`
	UserinfoEndpoint       string   `json:"userinfo_endpoint"`
	JWKSURI                string   `json:"jwks_uri"`
	SubjectTypesSupported  []string `json:"subject_types_supported"`
	ResponseTypesSupported []string `json:"response_types_supported"`
}

// IsValid reports whether doc has the minimum fields required by the OIDC
// Discovery 1.0 spec. userinfo_endpoint is RECOMMENDED rather than REQUIRED;
// callers that need it should check separately.
func (d *OIDCDiscoveryDoc) IsValid() bool {
	return d.Issuer != "" &&
		d.AuthorizationEndpoint != "" &&
		d.TokenEndpoint != "" &&
		d.JWKSURI != "" &&
		len(d.SubjectTypesSupported) > 0 &&
		len(d.ResponseTypesSupported) > 0
}
