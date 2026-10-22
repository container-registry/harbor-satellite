package migrations

import "embed"

// Schema keeps installed binaries independent of the source checkout.
// The migration files stay in this package because go:embed cannot
// reach outside the package directory.
//
//go:embed schema/*.sql
var Schema embed.FS
