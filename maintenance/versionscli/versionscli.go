// Package versionscli implements `agent --versions-list` and `agent --versions-switch` via REST API.
package versionscli

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/versionsapi"
)

// RunList runs: <bin> agent --versions-list [-apiprefix <path>] <self|local|remote-ip>
func RunList(args []string) int {
	apiPrefix, pos, showHelp, err := clirest.ParseAPIPrefixFlag(args)
	if showHelp {
		printVersionsListUsage()
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		printVersionsListUsage()
		return 1
	}
	if len(pos) != 1 {
		fmt.Fprintf(os.Stderr, "%s: expected exactly one argument: <self|local|remote-ip>\n", appmeta.BinaryName)
		printVersionsListUsage()
		return 1
	}

	target := strings.TrimSpace(pos[0])
	if err := clirest.ValidateTarget(target); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	client := clirest.DefaultHTTPClient(60 * time.Second)
	if err := clirest.EnsureServiceRunning(client, target, apiPrefix); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	base := clirest.APIBaseURL(target, apiPrefix)
	var payload struct {
		Versions []versionsapi.VersionEntry `json:"versions"`
	}
	if err := clirest.GetJSON(client, base+"/versions/list", &payload); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	remoteLabel := ""
	if !clirest.IsLocalTarget(target) {
		remoteLabel = target
	}
	printVersionsTable(os.Stdout, remoteLabel, payload.Versions)
	return 0
}

func printVersionsListUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s agent --versions-list [-apiprefix <path>] <self|local|remote-ip>\n\n", appmeta.BinaryName)
	fmt.Fprintf(os.Stderr, "  GET {APIPrefix}/versions/list on the target agent (Gin port %d).\n", clirest.DefaultHTTPPort)
	fmt.Fprintf(os.Stderr, "  Default -apiprefix: %s\n", clirest.DefaultAPIPrefix)
	fmt.Fprintf(os.Stderr, "  The target agent HTTP service must be running.\n\n")
}

func printVersionsTable(w io.Writer, remoteIP string, rows []versionsapi.VersionEntry) {
	if remoteIP != "" {
		fmt.Fprintf(w, "host %s\n", remoteIP)
	} else {
		fmt.Fprintln(w, "host self (local)")
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no versions)")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "VERSION\tCURRENT\tPREVIOUS")
	for _, r := range rows {
		cur, prev := "no", "no"
		if r.IsCurrent {
			cur = "yes"
		}
		if r.IsPrevious {
			prev = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Version, cur, prev)
	}
	_ = tw.Flush()
}

// RunSwitch runs: <bin> agent --versions-switch [-apiprefix <path>] <self|local|remote-ip> <version-key>
func RunSwitch(args []string) int {
	fs := flag.NewFlagSet("versions-switch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	apiPrefixFlag := fs.String("apiprefix", "", fmt.Sprintf("API path prefix (default %s)", clirest.DefaultAPIPrefix))
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent --versions-switch [-apiprefix <path>] <self|local|remote-ip> <version-key>\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  POST {APIPrefix}/versions/switch-current on the target agent (Gin port %d).\n", clirest.DefaultHTTPPort)
		fmt.Fprintf(os.Stderr, "  <version-key> may be \"previous\" to switch to the version marked PREVIOUS in versions-list.\n")
		fmt.Fprintf(os.Stderr, "  Default -apiprefix: %s\n", clirest.DefaultAPIPrefix)
		fmt.Fprintf(os.Stderr, "  The target agent HTTP service must be running.\n\n")
		fs.PrintDefaults()
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
		fmt.Fprintf(os.Stderr, "%s: expected two arguments: <self|local|remote-ip> <version-key|previous>\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}

	target := strings.TrimSpace(pos[0])
	version := strings.TrimSpace(pos[1])
	if target == "" || version == "" {
		fmt.Fprintf(os.Stderr, "%s: target and version must not be empty\n", appmeta.BinaryName)
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

	base := clirest.APIBaseURL(target, apiPrefix)
	resolved, err := resolveSwitchVersion(client, base, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}
	if strings.EqualFold(version, "previous") && resolved != version {
		fmt.Printf("Switching to previous version %s\n", resolved)
	}
	version = resolved
	if err := agentcfg.ValidateVersionKeyPath(version); err != nil {
		fmt.Fprintf(os.Stderr, "%s: invalid version key: %v\n", appmeta.BinaryName, err)
		return 1
	}

	body := map[string]string{"version": version}
	var msg string
	if err := clirest.PostJSON(client, base+"/versions/switch-current", body, &msg); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}
	if strings.TrimSpace(msg) != "" {
		fmt.Println(msg)
	} else {
		fmt.Println("Switch-current requested successfully.")
	}
	return 0
}

func resolveSwitchVersion(client *http.Client, base, versionArg string) (string, error) {
	versionArg = strings.TrimSpace(versionArg)
	if !strings.EqualFold(versionArg, "previous") {
		return versionArg, nil
	}
	var payload struct {
		Versions []versionsapi.VersionEntry `json:"versions"`
	}
	if err := clirest.GetJSON(client, base+"/versions/list", &payload); err != nil {
		return "", err
	}
	return versionsapi.PreviousVersionKeyFromEntries(payload.Versions)
}
