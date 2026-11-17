// Package web holds the offline view (HTML, CSS, JS) embedded in the binary.
package web

import _ "embed"

// Index is the page template; render replaces the __PLACEHOLDERS__.
//
//go:embed index.html
var Index string

// CSS is inlined into the page.
//
//go:embed app.css
var CSS string

// JS is inlined into the page.
//
//go:embed app.js
var JS string
