package web

import "embed"

// StaticFS holds the app icon, compiled CSS and vendored JavaScript under /static.
// app.css is produced by the Tailwind CLI (see Makefile target "css").
//
//go:embed assets/static
var StaticFS embed.FS
