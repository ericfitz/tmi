auth todo list

base url
/auth --> rename to /oauth2

login
GET /auth/login/{provider} --> GET /oauth2/authorize?idp={provider}&login_hint={login_hint}
and maybe (form encoding) --> POST /oauth2/authorize?idp={provider}&login_hint={login_hint}

token
(implicit) --> GET /oauth2/token

logout
POST /auth/logout --> POST /oauth2/revoke

me
GET /auth/me --> GET /oauth2/userinfo

introspect
(new) --> GET /oauth2/introspect

jwks
(new) --> GET /.well-known/jwks.json

provider configuration
GET /auth/providers --> GET /.well-known/openid-configuration
and --> GET /.well-known/oauth-authorization-server

scopes
openid profile email

claims:
name --> name
id --> sub
email --> email
+locale (rfc 5646, e.g. "en-US"/"en_US" )
+email_verified (boolean)

login request claims
iss - required issuer identifier (we will look up from provided idp value)
login_hint - optional hint to auth server about the end-user to be authenticated (test provider only)
target_link_uri - optional link to visit after authn/authz
scope - "openid"

JWT claims
iss (issuer)
sub (subject id)
aud (server app client id for provider)
exp (expiry time)
