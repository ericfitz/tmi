package config

// ProcessEnvVar documents one environment variable that a TMI binary reads
// straight from its process environment — outside the Config struct, the
// SettingDef registry and the config file. The registry cannot see these,
// so they are declared here by hand and rendered into config-reference.md
// (which doubles as the TMI_* allowlist) by GenerateReferenceMarkdown. The
// gate test in process_env_test.go fails when a TMI_* token appears in Go
// source without being declared here or in the registry (#810).
// SEM@2b405dc298a9b163f65c46256419a34afb630280: describe an env var a binary reads outside the config registry (pure)
type ProcessEnvVar struct {
	// Name is the exact variable name or, for a Pattern, the documented
	// shape with the operator-supplied part in angle brackets, e.g.
	// "TMI_SECRET_<KEY>"; the text before the first '<' is the prefix the
	// code scans for.
	Name string
	// Binary names the reader: "server" or "workers" (chunkembed,
	// extractor, worker-probe, component-controller).
	Binary string
	// Purpose is the one-line operator-facing description. No angle
	// brackets, backticks or pipes — it is rendered into a Markdown table.
	Purpose string
	// Secret marks a value that must never be logged or echoed.
	Secret bool
	// Pattern marks Name as a prefix pattern rather than a fixed name.
	Pattern bool
}

// processEnvVars is the hand-maintained inventory from #810. Fixed names
// are grouped by Binary in first-appearance order when rendered; patterns
// render in their own table.
var processEnvVars = []ProcessEnvVar{
	// --- server ---
	{Name: "TMI_ADMIN_PROVIDER", Binary: "server", Purpose: "Bootstrap administrator: identity provider. Setting it appends one entry to administrators at load time (internal/config)"},
	{Name: "TMI_ADMIN_PROVIDER_ID", Binary: "server", Purpose: "Bootstrap administrator: the subject's provider id"},
	{Name: "TMI_ADMIN_SUBJECT_TYPE", Binary: "server", Purpose: "Bootstrap administrator: subject type, user or group (default user)"},
	{Name: "TMI_ADMIN_EMAIL", Binary: "server", Purpose: "Bootstrap administrator: email address"},
	{Name: "TMI_ADMIN_GROUP_NAME", Binary: "server", Purpose: "Bootstrap administrator: group name when the subject type is group"},
	{Name: "TMI_JWT_KEY_ID", Binary: "server", Purpose: "JWKS key id; falls back to JWT_KEY_ID, then 1. Read by auth/config.go rather than internal/config"},
	{Name: "TMI_CLOUD_LOG_ENABLED", Binary: "server", Purpose: "Enable the cloud log writer when set to true"},
	{Name: "TMI_CLOUD_LOG_PROVIDER", Binary: "server", Purpose: "Cloud log provider; only oci is supported"},
	{Name: "TMI_CLOUD_LOG_LEVEL", Binary: "server", Purpose: "Minimum log level forwarded to the cloud log writer"},
	{Name: "TMI_OCI_LOG_ID", Binary: "server", Purpose: "OCI Logging log OCID the cloud log writer sends to"},
	{Name: "TMI_TEST_FORCE_AUTH_FLOW_RATE_LIMITING", Binary: "server", Purpose: "Test-only: force auth-flow rate limiting on. Honoured only when TMI_BUILD_MODE is test"},
	// SSRF overrides: cmd/server/main.go buildURIValidator reads these with
	// os.Getenv at startup, on top of the ssrf.* config settings.
	{Name: "TMI_SSRF_ISSUE_URI_ALLOWLIST", Binary: "server", Purpose: "Env override of the ssrf.issue_uri.allowlist setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_ISSUE_URI_SCHEMES", Binary: "server", Purpose: "Env override of the ssrf.issue_uri.schemes setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_DOCUMENT_URI_ALLOWLIST", Binary: "server", Purpose: "Env override of the ssrf.document_uri.allowlist setting (also used for content OAuth); read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_DOCUMENT_URI_SCHEMES", Binary: "server", Purpose: "Env override of the ssrf.document_uri.schemes setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_REPOSITORY_URI_ALLOWLIST", Binary: "server", Purpose: "Env override of the ssrf.repository_uri.allowlist setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_REPOSITORY_URI_SCHEMES", Binary: "server", Purpose: "Env override of the ssrf.repository_uri.schemes setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_TIMMY_ALLOWLIST", Binary: "server", Purpose: "Env override of the ssrf.timmy.allowlist setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_TIMMY_SCHEMES", Binary: "server", Purpose: "Env override of the ssrf.timmy.schemes setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_WEBHOOK_ALLOWLIST", Binary: "server", Purpose: "Env override of the ssrf.webhook.allowlist setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_WEBHOOK_SCHEMES", Binary: "server", Purpose: "Env override of the ssrf.webhook.schemes setting; read by cmd/server buildURIValidator at startup"},

	// --- workers: chunkembed, extractor, worker-probe, component-controller ---
	{Name: "TMI_NATS_URL", Binary: "workers", Purpose: "NATS endpoint. Required by every worker; also read by the server (extraction wiring) and component-controller (JetStream provisioning)"},
	{Name: "TMI_NATS_CREDS", Binary: "workers", Purpose: "Path to a NATS credentials file used when connecting (the file is secret; the path is not)"},
	{Name: "TMI_COMPONENT_NAME", Binary: "workers", Purpose: "This worker's TMIComponent name; required by chunkembed, extractor and worker-probe"},
	{Name: "TMI_HEARTBEAT_INTERVAL", Binary: "workers", Purpose: "Worker heartbeat period as a Go duration (chunkembed, extractor)"},
	{Name: "TMI_JOB_ACK_WAIT", Binary: "workers", Purpose: "JetStream ack wait per job as a Go duration (chunkembed, extractor); also a TMIComponent spec.config key read by component-controller"},
	{Name: "TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET", Binary: "workers", Purpose: "Extraction wall-clock cap read directly by the extractor worker; the same name is a registry setting for the server"},
	{Name: "TMI_EMBEDDING_MODEL", Binary: "workers", Purpose: "Embedding model name (chunkembed)"},
	{Name: "TMI_EMBEDDING_BASE_URL", Binary: "workers", Purpose: "Embedding API base URL (chunkembed)"},
	{Name: "TMI_EMBEDDING_API_KEY", Binary: "workers", Purpose: "Embedding API key (chunkembed)", Secret: true},
	{Name: "TMI_WORKER_NATS_URL", Binary: "workers", Purpose: "NATS URL for the worker bootstrap config; required (internal/config/bootstrap)"},
	{Name: "TMI_WORKER_LOG_LEVEL", Binary: "workers", Purpose: "Worker log level (internal/config/bootstrap)"},
	{Name: "TMI_WORKER_HEARTBEAT_SUBJECT", Binary: "workers", Purpose: "NATS subject worker heartbeats are published to (internal/config/bootstrap)"},

	// --- prefix patterns: the operator supplies the part in angle brackets ---
	{Name: "TMI_SECRET_<KEY>", Binary: "server", Pattern: true, Secret: true, Purpose: "Environment secrets provider: logical secret key, upper-cased, e.g. TMI_SECRET_JWT_SECRET or TMI_SECRET_SETTINGS_ENCRYPTION_KEY. Every value is a secret"},
	{Name: "TMI_WORKER_SECRET_MOUNT_<NAME>", Binary: "workers", Pattern: true, Purpose: "Filesystem path to a mounted secret file, exposed to the worker under the logical name, e.g. TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY"},
	{Name: "TMI_CONTENT_OAUTH_PROVIDERS_<ID>_<FIELD>", Binary: "server", Pattern: true, Purpose: "Per-provider content OAuth config, discovered from the ENABLED suffix. FIELD is one of CLIENT_ID, CLIENT_SECRET (secret), AUTH_URL, TOKEN_URL, USERINFO_URL, REVOCATION_URL, REQUIRED_SCOPES"},
	{Name: "SAML_PROVIDERS_<ID>_<FIELD>", Binary: "server", Pattern: true, Purpose: "Per-provider SAML settings (ENTITY_ID, ACS_URL, SP_PRIVATE_KEY, ...) discovered from the ENABLED suffix; note the name has no TMI prefix. FIELD is a SAMLProviderConfig yaml key upper-cased, e.g. ENTITY_ID, ACS_URL, SP_PRIVATE_KEY (secret), IDP_METADATA_B64XML (secret)"},
}

// ProcessEnvVars returns a copy of the process-environment inventory, so
// callers cannot mutate it.
// SEM@2b405dc298a9b163f65c46256419a34afb630280: list a defensive copy of the process-environment env var inventory (pure)
func ProcessEnvVars() []ProcessEnvVar {
	out := make([]ProcessEnvVar, len(processEnvVars))
	copy(out, processEnvVars)
	return out
}
