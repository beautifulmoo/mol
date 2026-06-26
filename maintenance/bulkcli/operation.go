package bulkcli

// Operation distinguishes bulk NDJSON progress and summary wording.
type Operation int

const (
	OpPushConfig Operation = iota
	OpRestart
	OpApplyUpdate
	OpRollback
)
