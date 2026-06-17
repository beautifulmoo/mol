// Package replcli implements interactive mode for agent CLI commands (<bin> agent).
package replcli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/applycli"
	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/hostinfo"
	"contrabass-agent/maintenance/hostinfocli"
	"contrabass-agent/maintenance/versionscli"

	"github.com/mattn/go-isatty"
)

var errReplExit = errors.New("repl exit")

// Run starts the interactive REPL. Invoked as: <bin> agent (or <bin> agent repl).
func Run(buildVersionKey, cliBuildVariant string, args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			printHelp("")
			return 0
		}
	}
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "%s: repl takes no arguments (use 'help' inside REPL)\n", appmeta.BinaryName)
		return 1
	}

	s := newSession(cliBuildVariant)
	v := strings.TrimSpace(buildVersionKey)
	if v == "" {
		v = "0.0.0-0"
	}
	fmt.Printf("%s %s interactive REPL. Type 'help' for commands, 'exit' to quit.\n", appmeta.BinaryName, v)
	fmt.Println("Bulk commands use hosts from the last 'discover'. Orchestrator maintenance must be running for bulk.")
	if isatty.IsTerminal(os.Stdin.Fd()) {
		fmt.Println("Arrow keys browse command history; Tab completes commands (saved under user cache).")
	}

	reader, err := newREPLReader(prompt, s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: readline: %v\n", appmeta.BinaryName, err)
		return 1
	}
	defer reader.Close()

	for {
		line, err := reader.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
			return 1
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := execLine(s, buildVersionKey, cliBuildVariant, line); err != nil {
			if errors.Is(err, errReplExit) {
				break
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
	return 0
}

func execLine(s *Session, buildVersionKey, cliBuildVariant, line string) error {
	fields := splitFields(line)
	if len(fields) == 0 {
		return nil
	}
	cmd := strings.ToLower(fields[0])
	args := fields[1:]

	switch cmd {
	case "exit", "quit":
		return errReplExit
	case "help":
		topic := ""
		if len(args) > 0 {
			topic = args[0]
		}
		printHelp(topic)
	case "version", "--version", "-version":
		v := strings.TrimSpace(buildVersionKey)
		if v == "" {
			v = "0.0.0-0"
		}
		fmt.Println(appmeta.BinaryName + " " + v)
	case "show", "settings":
		printSession(s)
	case "set":
		return runSet(s, args)
	case "hosts":
		printHosts(s)
	case "clear-hosts", "clear":
		if len(args) > 0 && strings.ToLower(args[0]) == "hosts" {
			s.clearHosts()
			fmt.Println("Cleared cached hosts.")
			return nil
		}
		if len(args) == 0 {
			s.clearHosts()
			fmt.Println("Cleared cached hosts.")
			return nil
		}
		return fmt.Errorf("usage: clear-hosts")
	case "discover", "discovery":
		return runDiscover(s, args)
	case "nic-brd", "nicbrd":
		runNicBrd()
	case "host-info", "hostinfo":
		if len(args) < 1 {
			return fmt.Errorf("usage: host-info <self|local|ip>")
		}
		code := hostinfocli.Run(buildVersionKey, cliBuildVariant, s.ginArgs(args[0]), &hostinfocli.RunOptions{
			DiscoveryCache: s.LastDiscovery,
		})
		if code != 0 {
			return fmt.Errorf("host-info failed (exit %d)", code)
		}
	case "versions-list":
		if len(args) < 1 {
			return fmt.Errorf("usage: versions-list <self|local|ip>")
		}
		code := versionscli.RunList(s.ginArgs(args[0]))
		if code != 0 {
			return fmt.Errorf("versions-list failed (exit %d)", code)
		}
	case "versions-switch":
		if len(args) < 2 {
			return fmt.Errorf("usage: versions-switch <self|local|ip> <version-key>")
		}
		code := versionscli.RunSwitch(s.ginArgs(args[0], args[1]))
		if code != 0 {
			return fmt.Errorf("versions-switch failed (exit %d)", code)
		}
	case "apply-update":
		rest, err := parseApplyUpdateArgs(s, args)
		if err != nil {
			return err
		}
		code := applycli.Run(buildVersionKey, cliBuildVariant, rest)
		if code != 0 {
			return fmt.Errorf("apply-update failed (exit %d)", code)
		}
	case "push-config-all", "push-config-all-remotes":
		return runPushConfigAll(s)
	case "restart-all", "restart-all-remotes":
		return runRestartAll(s)
	case "apply-update-all", "apply-update-all-remotes":
		bundle, useBundle, agentVar, err := parseApplyUpdateAllArgs(s, args)
		if err != nil {
			return err
		}
		return runApplyUpdateAll(s, bundle, agentVar, useBundle)
	case "rollback-all", "rollback-all-remotes":
		return runRollbackAll(s)
	default:
		return fmt.Errorf("unknown command %q (type 'help')", cmd)
	}
	return nil
}

func parseApplyUpdateArgs(s *Session, args []string) ([]string, error) {
	useBundle := s.UseBundleConfig
	agentVar := strings.TrimSpace(s.AgentVariant)
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-use-bundle-config" || a == "--use-bundle-config":
			useBundle = true
		case strings.HasPrefix(a, "-agent-variant="):
			agentVar = strings.TrimPrefix(a, "-agent-variant=")
		case a == "-agent-variant" && i+1 < len(args):
			i++
			agentVar = args[i]
		case strings.HasPrefix(a, "-apiprefix="):
			// one-off override not supported; use set apiprefix
			return nil, fmt.Errorf("use 'set apiprefix' for session default (not per-command -apiprefix)")
		default:
			positional = append(positional, a)
		}
	}
	if len(positional) != 2 {
		return nil, fmt.Errorf("usage: apply-update [-agent-variant=control|compute] [-use-bundle-config] <self|local|ip> <bundle.tar.gz>")
	}
	return s.applyUpdateCLIArgs(positional[0], positional[1], agentVar, useBundle), nil
}

func parseApplyUpdateAllArgs(s *Session, args []string) (bundle string, useBundle bool, agentVariant string, err error) {
	useBundle = s.UseBundleConfig
	agentVariant = strings.TrimSpace(s.AgentVariant)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-use-bundle-config" || a == "--use-bundle-config":
			useBundle = true
		case strings.HasPrefix(a, "-agent-variant="):
			agentVariant = strings.TrimPrefix(a, "-agent-variant=")
		case a == "-agent-variant" && i+1 < len(args):
			i++
			agentVariant = args[i]
		default:
			if bundle != "" {
				return "", false, "", fmt.Errorf("unexpected argument: %q", a)
			}
			bundle = a
		}
	}
	if strings.TrimSpace(bundle) == "" {
		return "", false, "", fmt.Errorf("usage: apply-update-all [-agent-variant=...] [-use-bundle-config] <bundle.tar.gz>")
	}
	if agentVariant != "" {
		if _, err := clirest.ResolveAgentVariantForTarget(agentVariant, ""); err != nil {
			return "", false, "", err
		}
	}
	return bundle, useBundle, agentVariant, nil
}

func runNicBrd() {
	for _, p := range hostinfo.GetPhysicalNICBrdPairs() {
		fmt.Printf("%s : %s\n", p.Iface, p.Brd)
	}
}
