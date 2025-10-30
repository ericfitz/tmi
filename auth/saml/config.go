package saml

// SAMLConfig represents the configuration for a SAML identity provider
type SAMLConfig struct {
	// Basic configuration
	ID      string `json:"id" yaml:"id"`           // Provider ID (e.g., "saml_okta")
	Name    string `json:"name" yaml:"name"`       // Display name
	Enabled bool   `json:"enabled" yaml:"enabled"` // Whether this provider is enabled
	Icon    string `json:"icon" yaml:"icon"`       // Icon identifier

	// SAML-specific configuration
	EntityID          string `json:"entity_id" yaml:"entity_id"`                     // Service Provider entity ID
	ACSURL            string `json:"acs_url" yaml:"acs_url"`                         // Assertion Consumer Service URL
	SLOURL            string `json:"slo_url" yaml:"slo_url"`                         // Single Logout URL
	IDPMetadataURL    string `json:"idp_metadata_url" yaml:"idp_metadata_url"`       // IdP metadata URL
	IDPMetadataXML    string `json:"idp_metadata_xml" yaml:"idp_metadata_xml"`       // Alternative: static IdP metadata
	SPPrivateKeyPath  string `json:"sp_private_key_path" yaml:"sp_private_key_path"` // SP private key file path
	SPCertificatePath string `json:"sp_certificate_path" yaml:"sp_certificate_path"` // SP certificate file path
	SPPrivateKey      string `json:"sp_private_key" yaml:"sp_private_key"`           // SP private key (PEM)
	SPCertificate     string `json:"sp_certificate" yaml:"sp_certificate"`           // SP certificate (PEM)
	AllowIDPInitiated bool   `json:"allow_idp_initiated" yaml:"allow_idp_initiated"` // Allow IdP-initiated SSO
	ForceAuthn        bool   `json:"force_authn" yaml:"force_authn"`                 // Force reauthentication
	SignRequests      bool   `json:"sign_requests" yaml:"sign_requests"`             // Sign AuthnRequests
	EncryptAssertions bool   `json:"encrypt_assertions" yaml:"encrypt_assertions"`   // Require encrypted assertions

	// Attribute mapping
	AttributeMapping map[string]string `json:"attribute_mapping" yaml:"attribute_mapping"`

	// Group configuration
	GroupAttributeName string `json:"group_attribute_name" yaml:"group_attribute_name"` // SAML attribute containing groups
	GroupPrefix        string `json:"group_prefix" yaml:"group_prefix"`                 // Optional prefix filter for groups
}

// AttributeNames defines standard SAML attribute names
type AttributeNames struct {
	Email      string
	Name       string
	GivenName  string
	FamilyName string
	Groups     string
	UID        string
}

// DefaultAttributeNames returns common SAML attribute names
func DefaultAttributeNames() AttributeNames {
	return AttributeNames{
		Email:      "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
		Name:       "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
		GivenName:  "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname",
		FamilyName: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname",
		Groups:     "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
		UID:        "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/nameidentifier",
	}
}

// OktaAttributeNames returns Okta-specific SAML attribute names
func OktaAttributeNames() AttributeNames {
	return AttributeNames{
		Email:      "email",
		Name:       "name",
		GivenName:  "firstName",
		FamilyName: "lastName",
		Groups:     "memberOf",
		UID:        "login",
	}
}
