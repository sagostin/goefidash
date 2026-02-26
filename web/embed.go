package web

import "embed"

// FS contains all embedded web assets (HTML, CSS, JS).
//
//go:embed *.html *.css *.js
var FS embed.FS
