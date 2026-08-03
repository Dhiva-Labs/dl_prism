// Package ui embeds the built web dashboard (see docs/UI_PLAN.md).
// Run `npm run build` in this directory to refresh dist/.
package ui

import "embed"

//go:embed all:dist
var Dist embed.FS
