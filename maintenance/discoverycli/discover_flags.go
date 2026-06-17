package discoverycli

import (
	"fmt"
	"strconv"
	"strings"

	"contrabass-agent/maintenance/agentcfg"
)

// ParseDiscoverArgs parses REPL-style discover flags into DiscoverOptions.
func ParseDiscoverArgs(args []string) (DiscoverOptions, error) {
	opts := DiscoverOptions{
		DestPort:    DefaultDestPort,
		SrcPort:     DefaultSrcPort,
		TimeoutSec:  DefaultTimeoutSec,
		ServiceName: agentcfg.DefaultDiscoveryServiceName,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "--dest-port="):
			n, err := parseDiscoverPortFlag(a, "--dest-port=")
			if err != nil {
				return opts, err
			}
			opts.DestPort = n
		case strings.HasPrefix(a, "--src-port="):
			n, err := parseDiscoverPortFlag(a, "--src-port=")
			if err != nil {
				return opts, err
			}
			opts.SrcPort = n
		case strings.HasPrefix(a, "--timeout="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--timeout="))
			if err != nil || n <= 0 {
				return opts, fmt.Errorf("--timeout must be positive")
			}
			opts.TimeoutSec = n
		case strings.HasPrefix(a, "--service="):
			opts.ServiceName = strings.TrimPrefix(a, "--service=")
		case a == "--dest-port" && i+1 < len(args):
			i++
			n, err := parseDiscoverPortArg(args[i])
			if err != nil {
				return opts, err
			}
			opts.DestPort = n
		case a == "--src-port" && i+1 < len(args):
			i++
			n, err := parseDiscoverPortArg(args[i])
			if err != nil {
				return opts, err
			}
			opts.SrcPort = n
		case a == "--timeout" && i+1 < len(args):
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return opts, fmt.Errorf("--timeout must be positive")
			}
			opts.TimeoutSec = n
		case a == "--service" && i+1 < len(args):
			i++
			opts.ServiceName = args[i]
		default:
			return opts, fmt.Errorf("unknown discover flag: %q", a)
		}
	}
	return opts, nil
}

func parseDiscoverPortFlag(a, prefix string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(a, prefix))
	if err != nil || n <= 0 || n > 65535 {
		return 0, fmt.Errorf("%s must be 1..65535", strings.TrimSuffix(prefix, "="))
	}
	return n, nil
}

func parseDiscoverPortArg(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 65535 {
		return 0, fmt.Errorf("port must be 1..65535")
	}
	return n, nil
}
