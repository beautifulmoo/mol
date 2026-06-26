package bulkcli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discoverycli"
)

// RegisterMaintenanceFlags adds -apiprefix and -maintenance-port to fs (shared by standalone bulk CLIs).
func RegisterMaintenanceFlags(fs *flag.FlagSet) (*string, *int) {
	apiPrefix := fs.String("apiprefix", "", fmt.Sprintf("maintenance API path prefix (default %s)", clirest.DefaultAPIPrefix))
	maintenancePort := fs.Int("maintenance-port", clirest.DefaultMaintenancePort, "local maintenance HTTP port (orchestrator)")
	return apiPrefix, maintenancePort
}

// WriteDiscoveryBuiltInHelp documents the built-in UDP discovery prelude.
func WriteDiscoveryBuiltInHelp(w io.Writer) {
	fmt.Fprintf(w, "  Discovery runs automatically in this command (dest-port %d, src-port %d, timeout %ds, service %q).\n",
		discoverycli.DefaultDestPort, discoverycli.DefaultSrcPort, discoverycli.DefaultTimeoutSec, "Mole-Discovery")
	fmt.Fprintf(w, "  A separate agent --discovery run is not required.\n")
	fmt.Fprintf(w, "  Discovery flags (--dest-port, --src-port, --timeout, --service) are not on this command.\n")
	fmt.Fprintf(w, "  To probe with custom discovery settings only, use %s agent --discovery (standalone).\n\n", appmeta.BinaryName)
}

// WriteMaintenanceFlagHelp prints shared flag documentation.
func WriteMaintenanceFlagHelp(w io.Writer) {
	fmt.Fprintf(w, "  -apiprefix=<path>\n        API path prefix (default %s)\n", clirest.DefaultAPIPrefix)
	fmt.Fprintf(w, "  -maintenance-port=N\n        local maintenance HTTP port (default %d)\n", clirest.DefaultMaintenancePort)
}

// ValidateMaintenancePort returns an error when port is out of range.
func ValidateMaintenancePort(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("-maintenance-port must be 1..65535")
	}
	return nil
}

// CheckHelpArgs returns true when args request -h/--help (before flag parse).
func CheckHelpArgs(args []string, usage func()) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			usage()
			return true
		}
	}
	return false
}

// FormatMaintenancePortError formats a maintenance port validation failure.
func FormatMaintenancePortError(err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
}
