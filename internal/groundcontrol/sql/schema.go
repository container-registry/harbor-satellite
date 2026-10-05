package migrations

import "embed"

// Schema keeps installed binaries independent of the source checkout.
//
//go:embed schema/*.sql
var Schema embed.FS
