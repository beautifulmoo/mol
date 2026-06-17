// Package applycli implements `contrabass-moleU agent --apply-update` via REST (upload + apply-update).
package applycli

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/clirest"
)

// Run parses flags and runs apply-update CLI.
//
//	<bin> agent --apply-update [-apiprefix <path>] [-agent-variant=control|compute] [-use-bundle-config] <self|local|remote-ip> <bundle.tar.gz>
func Run(buildVersionKey, cliBuildVariant string, args []string) int {
	_ = buildVersionKey

	fs := flag.NewFlagSet("apply-update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	apiPrefixFlag := fs.String("apiprefix", "", fmt.Sprintf("API path prefix (default %s)", clirest.DefaultAPIPrefix))
	agentVariantFlag := fs.String("agent-variant", "", "control or compute; if omitted, match installed build_variant on target (compute if unknown)")
	useBundleConfigFlag := fs.Bool("use-bundle-config", false, "apply config from the bundle instead of reusing current agent.local.yml")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent --apply-update [-apiprefix=<path>] [-agent-variant=control|compute] [-use-bundle-config] <self|local|remote-ip> <bundle.tar.gz>\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  POST {APIPrefix}/upload then POST {APIPrefix}/apply-update on the target agent (Gin port %d).\n", clirest.DefaultHTTPPort)
		fmt.Fprintf(os.Stderr, "  The target agent HTTP service must be running.\n\n")
		fmt.Fprintf(os.Stderr, "  Optional flags may be written as -name=value or -name value (Go flag package).\n\n")
		fmt.Fprintf(os.Stderr, "  -apiprefix=<path>\n")
		fmt.Fprintf(os.Stderr, "        API path prefix (default %s)\n", clirest.DefaultAPIPrefix)
		fmt.Fprintf(os.Stderr, "  -agent-variant=control|compute\n")
		fmt.Fprintf(os.Stderr, "        which bundle binary becomes %s at apply; if omitted, match installed\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "        build_variant on target (compute if unknown)\n")
		fmt.Fprintf(os.Stderr, "  -use-bundle-config\n")
		fmt.Fprintf(os.Stderr, "        use agent.local.yml from the bundle; default is to reuse the target's\n")
		fmt.Fprintf(os.Stderr, "        current config (same as the web UI reuse checkbox, on by default)\n")
	}
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fs.Usage()
			return 0
		}
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	pos := fs.Args()
	if len(pos) != 2 {
		fmt.Fprintf(os.Stderr, "%s: expected two arguments: <self|local|remote-ip> <bundle.tar.gz>\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}

	target := strings.TrimSpace(pos[0])
	bundlePath := strings.TrimSpace(pos[1])
	if target == "" || bundlePath == "" {
		fmt.Fprintf(os.Stderr, "%s: target and bundle path must not be empty\n", appmeta.BinaryName)
		return 1
	}
	if err := clirest.ValidateTarget(target); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	client := clirest.DefaultHTTPClient(300 * time.Second)
	apiPrefix := *apiPrefixFlag
	if err := clirest.EnsureServiceRunning(client, target, apiPrefix); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	selfInfo, err := clirest.GetSelf(client, target, apiPrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: get target version: %v\n", appmeta.BinaryName, err)
		return 1
	}
	installedBV := strings.TrimSpace(selfInfo.BuildVariant)
	if installedBV == "" {
		installedBV = strings.TrimSpace(cliBuildVariant)
	}
	if installedBV == "" {
		installedBV = "compute"
	}
	variant, err := clirest.ResolveAgentVariantForTarget(*agentVariantFlag, installedBV)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	versionKey, err := clirest.UploadBundle(client, target, apiPrefix, bundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: upload: %v\n", appmeta.BinaryName, err)
		return 1
	}

	cur := strings.TrimSpace(selfInfo.Version)
	reusePreviousConfig := !*useBundleConfigFlag
	if clirest.IsLocalTarget(target) {
		fmt.Printf("Applying bundle %s locally (current %s, variant %s, reuse_previous_config=%v)\n", versionKey, cur, variant, reusePreviousConfig)
	} else {
		fmt.Printf("Applying bundle %s to remote %s (current %s, variant %s, reuse_previous_config=%v)\n", versionKey, target, cur, variant, reusePreviousConfig)
	}

	msg, err := clirest.ApplyUpdateJSON(client, target, apiPrefix, versionKey, variant, reusePreviousConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: apply: %v\n", appmeta.BinaryName, err)
		return 1
	}
	if strings.TrimSpace(msg) != "" {
		fmt.Println(msg)
	} else {
		fmt.Println("Apply update requested; the agent will restart shortly.")
	}
	return 0
}
