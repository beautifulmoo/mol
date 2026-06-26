package bulkcli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"contrabass-agent/maintenance/clirest"
)

// Counters tracks bulk NDJSON progress tallies.
type Counters struct {
	Succeeded, Failed, Skipped, Total int
}

// ProgressHandler returns an onEvent callback for clirest bulk NDJSON APIs.
func ProgressHandler(op Operation, c *Counters) func(map[string]interface{}) error {
	return func(evt map[string]interface{}) error {
		return printBulkProgress(evt, op, c)
	}
}

func printBulkProgress(evt map[string]interface{}, op Operation, c *Counters) error {
	typ, _ := evt["type"].(string)
	switch typ {
	case "start":
		if t, ok := evt["total"].(float64); ok {
			c.Total = int(t)
		}
	case "progress":
		status, _ := evt["status"].(string)
		ip, _ := evt["ip"].(string)
		hostname, _ := evt["hostname"].(string)
		label := clirest.FormatBulkHostLabel(hostname, ip)
		cur, _ := evt["current"].(float64)
		tot, _ := evt["total"].(float64)
		prefix := fmt.Sprintf("[%d/%d] %s: ", int(cur), int(tot), label)
		switch status {
		case "success":
			c.Succeeded++
			suffix := connectViaSuffix(evt)
			switch op {
			case OpPushConfig:
				fmt.Println(prefix + "success" + suffix)
			case OpRestart:
				detail := verifyDetailSuffix(evt)
				fmt.Println(prefix + "restart verified" + suffix + detail)
			case OpApplyUpdate:
				ver := versionSuffix(evt)
				fmt.Println(prefix + "update apply requested" + ver + suffix)
			case OpRollback:
				fmt.Println(prefix + "rollback requested" + suffix)
			}
		case "skipped":
			c.Skipped++
			msg, _ := evt["message"].(string)
			if strings.TrimSpace(msg) == "" {
				msg = "not applicable"
			}
			fmt.Println(prefix + "skipped — " + msg)
		case "fail":
			c.Failed++
			msg, _ := evt["message"].(string)
			if strings.TrimSpace(msg) == "" {
				if op == OpRestart {
					if vd, _ := evt["verify_detail"].(string); strings.TrimSpace(vd) != "" {
						msg = strings.TrimSpace(vd)
					}
				}
				if strings.TrimSpace(msg) == "" {
					msg = "unknown error"
				}
			}
			fmt.Println(prefix + "fail — " + msg)
		default:
			fmt.Println(prefix + status)
		}
	case "done":
		if sv, ok := evt["succeeded"].(float64); ok {
			c.Succeeded = int(sv)
		}
		if f, ok := evt["failed"].(float64); ok {
			c.Failed = int(f)
		}
		if sk, ok := evt["skipped"].(float64); ok {
			c.Skipped = int(sk)
		}
		if t, ok := evt["total"].(float64); ok {
			c.Total = int(t)
		}
	}
	return nil
}

func connectViaSuffix(evt map[string]interface{}) string {
	if connectIP, _ := evt["connect_ip"].(string); strings.TrimSpace(connectIP) != "" {
		return " via " + strings.TrimSpace(connectIP)
	}
	return ""
}

func verifyDetailSuffix(evt map[string]interface{}) string {
	if vd, _ := evt["verify_detail"].(string); strings.TrimSpace(vd) != "" {
		return " — " + strings.TrimSpace(vd)
	}
	return ""
}

func versionSuffix(evt map[string]interface{}) string {
	if v, _ := evt["version"].(string); strings.TrimSpace(v) != "" {
		return " (" + strings.TrimSpace(v) + ")"
	}
	return ""
}

// FinishSummary prints the final summary and returns an error when the command should fail.
func FinishSummary(op Operation, c Counters, w io.Writer) error {
	if w == nil {
		w = os.Stdout
	}
	if c.Total == 0 {
		fmt.Fprintln(w, "No hosts were processed.")
		return fmt.Errorf("no hosts were processed")
	}
	if c.Failed > 0 || c.Skipped > 0 {
		if c.Skipped > 0 {
			fmt.Fprintf(w, "Done: succeeded=%d failed=%d skipped=%d (total %d).\n", c.Succeeded, c.Failed, c.Skipped, c.Total)
		} else {
			fmt.Fprintf(w, "Done: succeeded=%d failed=%d (total %d).\n", c.Succeeded, c.Failed, c.Total)
		}
	} else {
		switch op {
		case OpPushConfig:
			fmt.Fprintf(w, "Done: all %d host(s) succeeded.\n", c.Succeeded)
		case OpRestart:
			fmt.Fprintf(w, "Done: all %d host(s) restarted successfully.\n", c.Succeeded)
		case OpApplyUpdate:
			fmt.Fprintf(w, "Done: all %d host(s) received update apply.\n", c.Succeeded)
		case OpRollback:
			fmt.Fprintf(w, "Done: all %d host(s) received rollback.\n", c.Succeeded)
		}
	}
	if c.Failed > 0 {
		return fmt.Errorf("command finished with errors")
	}
	if (op == OpApplyUpdate || op == OpRollback) && c.Succeeded == 0 {
		return fmt.Errorf("command finished with errors")
	}
	return nil
}
