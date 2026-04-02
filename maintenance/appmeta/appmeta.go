package appmeta

// BinaryName is the user-facing executable name used in help/error messages.
// Keep this as the single place to rename the binary in output strings.
const BinaryName = "contrabass-moleU"

// UpdateTransientUnitStem is the name passed to systemd-run --unit= for one-off update script jobs.
// It is not the main agent unit (see SystemctlServiceName / contrabass-mole.service).
const UpdateTransientUnitStem = "contrabass-mole-update"

// UpdateTransientUnit is the full transient unit name for systemctl (e.g. is-active, reset-failed).
const UpdateTransientUnit = UpdateTransientUnitStem + ".service"

