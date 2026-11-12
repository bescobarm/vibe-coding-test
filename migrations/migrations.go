// Package migrations embeds the SQL migrations so the engine binary can apply
// them at startup without any external tool (D-022).
package migrations

import "embed"

// FS holds every *.sql file in this directory. Files are applied in name order.
//
//go:embed *.sql
var FS embed.FS
