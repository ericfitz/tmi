// Package tmi embeds static assets that the TMI server serves under /static.
//
// The assets (favicons, web manifest icons, logos, and OAuth/SAML provider
// sign-in icons) are embedded into the binary rather than read from a relative
// ./static directory at runtime. The server runs from a Chainguard "static"
// container that contains only the binary (see Dockerfile.server), so a
// relative ./static path resolves to nothing and every static route 404s in
// production — including the provider sign-in icons (#498). Embedding keeps the
// image binary-only while making the assets available in every deployment
// topology regardless of the working directory.
package tmi

import "embed"

// StaticFS holds the contents of the static/ directory, embedded at build time.
// Serve it under /static via fs.Sub(StaticFS, "static").
//
//go:embed all:static
var StaticFS embed.FS
