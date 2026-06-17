package replcli

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"contrabass-agent/maintenance/clirest"
)

func runPushConfigAll(s *Session) error {
	return runCachedBulk(s, "Pushing local current config to %d cached host(s)...\n", bulkKindPush, clirest.PushLocalConfigAll)
}

func runRestartAll(s *Session) error {
	return runCachedBulk(s, "Restarting %d cached host(s)...\n", bulkKindRestart, clirest.RestartAllRemotes)
}

func runRollbackAll(s *Session) error {
	return runCachedBulk(s, "Rolling back %d cached host(s)...\n", bulkKindRollback, clirest.RollbackAllRemotes)
}

func runCachedBulk(s *Session, startFmt string, kind bulkKind, run bulkRunner) error {
	hosts, err := s.requireCachedHosts()
	if err != nil {
		return err
	}
	check := clirest.DefaultHTTPClient(5 * time.Second)
	if err := clirest.EnsureMaintenanceRunning(check, s.APIPrefix, s.MaintenancePort); err != nil {
		return err
	}
	fmt.Printf(startFmt, len(hosts))
	return runBulkNDJSON(run, s, hosts, kind)
}

func runApplyUpdateAll(s *Session, bundlePath, agentVariantOverride string, useBundleOverride bool) error {
	hosts, err := s.requireCachedHosts()
	if err != nil {
		return err
	}
	check := clirest.DefaultHTTPClient(5 * time.Second)
	if err := clirest.EnsureMaintenanceRunning(check, s.APIPrefix, s.MaintenancePort); err != nil {
		return err
	}

	fmt.Printf("Uploading bundle %s to local staging...\n", bundlePath)
	uploadClient := clirest.DefaultHTTPClient(300 * time.Second)
	versionKey, err := clirest.UploadBundleMaintenance(uploadClient, s.APIPrefix, s.MaintenancePort, bundlePath)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	agentVariant := strings.TrimSpace(agentVariantOverride)
	if agentVariant == "" {
		agentVariant = strings.TrimSpace(s.AgentVariant)
	}
	if agentVariant == "" {
		agentVariant = s.CLIBuildVariant
	}
	if agentVariant == "" {
		agentVariant = "compute"
	}
	variant, err := clirest.ResolveAgentVariantForTarget(agentVariant, s.CLIBuildVariant)
	if err != nil {
		return err
	}
	reusePrevious := !useBundleOverride
	fmt.Printf("Staged version %s (variant %s, reuse_previous_config=%v).\n", versionKey, variant, reusePrevious)
	fmt.Printf("Applying version %s to %d cached host(s)...\n", versionKey, len(hosts))

	streamClient := clirest.DefaultHTTPClient(0)
	opts := clirest.ApplyUpdateAllOptions{
		Version:             versionKey,
		AgentVariant:        variant,
		ReusePreviousConfig: reusePrevious,
	}
	var succeeded, failed, skipped int
	var doneTotal int
	err = clirest.ApplyUpdateAll(streamClient, s.APIPrefix, s.MaintenancePort, hosts, opts, func(evt map[string]interface{}) error {
		return printBulkProgress(evt, bulkKindApply, &succeeded, &failed, &skipped, &doneTotal)
	})
	if err != nil {
		return err
	}
	return finishBulkSummary(bulkKindApply, succeeded, failed, skipped, doneTotal)
}

type bulkKind int

const (
	bulkKindPush bulkKind = iota
	bulkKindRestart
	bulkKindApply
	bulkKindRollback
)

type bulkRunner func(client *http.Client, apiPrefix string, maintenancePort int, hosts []clirest.BulkPushHost, onEvent func(map[string]interface{}) error) error

func runBulkNDJSON(run bulkRunner, s *Session, hosts []clirest.BulkPushHost, kind bulkKind) error {
	var succeeded, failed, skipped int
	var doneTotal int
	streamClient := clirest.DefaultHTTPClient(0)
	err := run(streamClient, s.APIPrefix, s.MaintenancePort, hosts, func(evt map[string]interface{}) error {
		return printBulkProgress(evt, kind, &succeeded, &failed, &skipped, &doneTotal)
	})
	if err != nil {
		return err
	}
	return finishBulkSummary(kind, succeeded, failed, skipped, doneTotal)
}

func printBulkProgress(evt map[string]interface{}, kind bulkKind, succeeded, failed, skipped, doneTotal *int) error {
	typ, _ := evt["type"].(string)
	switch typ {
	case "start":
		if t, ok := evt["total"].(float64); ok {
			*doneTotal = int(t)
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
			*succeeded++
			suffix := connectViaSuffix(evt)
			switch kind {
			case bulkKindPush:
				fmt.Println(prefix + "success" + suffix)
			case bulkKindRestart:
				detail := verifyDetailSuffix(evt)
				fmt.Println(prefix + "restart verified" + suffix + detail)
			case bulkKindApply:
				ver := versionSuffix(evt)
				fmt.Println(prefix + "update apply requested" + ver + suffix)
			case bulkKindRollback:
				fmt.Println(prefix + "rollback requested" + suffix)
			}
		case "skipped":
			*skipped++
			msg, _ := evt["message"].(string)
			if strings.TrimSpace(msg) == "" {
				msg = "not applicable"
			}
			fmt.Println(prefix + "skipped — " + msg)
		case "fail":
			*failed++
			msg, _ := evt["message"].(string)
			if strings.TrimSpace(msg) == "" {
				msg = "unknown error"
			}
			fmt.Println(prefix + "fail — " + msg)
		default:
			fmt.Println(prefix + status)
		}
	case "done":
		if sv, ok := evt["succeeded"].(float64); ok {
			*succeeded = int(sv)
		}
		if f, ok := evt["failed"].(float64); ok {
			*failed = int(f)
		}
		if sk, ok := evt["skipped"].(float64); ok {
			*skipped = int(sk)
		}
		if t, ok := evt["total"].(float64); ok {
			*doneTotal = int(t)
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

func finishBulkSummary(kind bulkKind, succeeded, failed, skipped, doneTotal int) error {
	if doneTotal == 0 {
		return fmt.Errorf("no hosts were processed")
	}
	if skipped > 0 || failed > 0 {
		fmt.Fprintf(os.Stdout, "Done: succeeded=%d failed=%d skipped=%d (total %d).\n", succeeded, failed, skipped, doneTotal)
	} else if kind == bulkKindPush || kind == bulkKindRestart {
		fmt.Fprintf(os.Stdout, "Done: all %d host(s) succeeded.\n", succeeded)
	} else if kind == bulkKindApply {
		fmt.Fprintf(os.Stdout, "Done: all %d host(s) received update apply.\n", succeeded)
	} else {
		fmt.Fprintf(os.Stdout, "Done: all %d host(s) received rollback.\n", succeeded)
	}
	if failed > 0 {
		return fmt.Errorf("command finished with errors")
	}
	if (kind == bulkKindApply || kind == bulkKindRollback) && succeeded == 0 {
		return fmt.Errorf("command finished with errors")
	}
	return nil
}
