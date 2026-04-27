package updatescripts

import _ "embed"

// UpdateSh is update.sh (repository root), embedded at build time.
//
//go:embed update.sh
var UpdateSh string

// RollbackSh is rollback.sh (repository root), embedded at build time.
//
//go:embed rollback.sh
var RollbackSh string
