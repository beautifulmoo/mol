package replcli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chzyer/readline"
)

// replCommands lists primary REPL command names for tab completion.
var replCommands = []string{
	"apply-update",
	"apply-update-all",
	"clear-hosts",
	"discover",
	"exit",
	"help",
	"host-info",
	"hosts",
	"nic-brd",
	"push-config-all",
	"quit",
	"restart-all",
	"rollback-all",
	"set",
	"show",
	"version",
	"versions-list",
	"versions-switch",
}

var replSetKeys = []string{
	"agent-variant",
	"apiprefix",
	"maintenance-port",
	"use-bundle-config",
}

var replDiscoverFlags = []string{
	"--dest-port=",
	"--src-port=",
	"--timeout=",
	"--service=",
}

type replCompleter struct {
	session *Session
}

func (c *replCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if pos < 0 || pos > len(line) {
		return nil, 0
	}
	wordStart, word := currentWord(line, pos)
	prefix := strings.TrimSpace(string(line[:wordStart]))
	tokens := splitFields(prefix)
	candidates := c.candidates(tokens, word)
	suffixes, length := formatCompleterSuffixes(word, candidates)
	return suffixes, length
}

func newReplCompleter(s *Session) readline.AutoCompleter {
	return &replCompleter{session: s}
}

func currentWord(line []rune, pos int) (start int, word string) {
	start = pos
	for start > 0 && line[start-1] != ' ' && line[start-1] != '\t' {
		start--
	}
	return start, string(line[start:pos])
}

func (c *replCompleter) candidates(tokens []string, word string) []string {
	if len(tokens) == 0 {
		return prefixMatches(replCommands, word)
	}
	cmd := strings.ToLower(tokens[0])
	switch cmd {
	case "help":
		if len(tokens) == 1 {
			return prefixMatches(helpTopicNames(), word)
		}
	case "set":
		if len(tokens) == 1 {
			return prefixMatches(replSetKeys, word)
		}
		if len(tokens) == 2 {
			return prefixMatches(setValueCandidates(tokens[1]), word)
		}
	case "clear", "clear-hosts":
		if len(tokens) == 1 {
			return prefixMatches([]string{"hosts"}, word)
		}
	case "discover", "discovery":
		if len(tokens) >= 1 {
			return prefixMatches(replDiscoverFlags, word)
		}
	case "host-info", "hostinfo", "versions-list", "versions-switch", "apply-update":
		if len(tokens) == 1 {
			return prefixMatches(c.targetCandidates(), word)
		}
		if cmd == "apply-update" && len(tokens) == 2 {
			return prefixMatches(filePathCandidates(word), word)
		}
	}
	return nil
}

func helpTopicNames() []string {
	names := make([]string, 0, len(helpTopics)+len(replCommands))
	for k := range helpTopics {
		names = append(names, k)
	}
	names = append(names, replCommands...)
	sort.Strings(names)
	return uniqueSorted(names)
}

func setValueCandidates(key string) []string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "agent-variant", "agent_variant":
		return []string{"control", "compute"}
	case "use-bundle-config", "use_bundle_config":
		return []string{"on", "off", "true", "false", "yes", "no"}
	default:
		return nil
	}
}

func (c *replCompleter) targetCandidates() []string {
	out := []string{"self", "local"}
	if c.session != nil {
		seen := map[string]struct{}{"self": {}, "local": {}}
		for _, h := range c.session.CachedHosts {
			add := func(ip string) {
				ip = strings.TrimSpace(ip)
				if ip == "" {
					return
				}
				if _, ok := seen[ip]; ok {
					return
				}
				seen[ip] = struct{}{}
				out = append(out, ip)
			}
			add(h.PrimaryIP)
			for _, ip := range h.IPs {
				add(ip)
			}
		}
	}
	sort.Strings(out[2:])
	return out
}

func filePathCandidates(partial string) []string {
	partial = strings.TrimSpace(partial)
	dir := "."
	base := partial
	if partial != "" {
		dir = filepath.Dir(partial)
		if dir == "." || dir == "" {
			dir = "."
		}
		base = filepath.Base(partial)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if partial == "" || strings.HasPrefix(name, base) {
			full := name
			if dir != "." {
				full = filepath.Join(dir, name)
			}
			out = append(out, full)
		}
	}
	sort.Strings(out)
	return out
}

func prefixMatches(candidates []string, word string) []string {
	wordLower := strings.ToLower(word)
	var out []string
	for _, c := range candidates {
		if word == "" || strings.HasPrefix(strings.ToLower(c), wordLower) {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return uniqueSorted(out)
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	sort.Strings(in)
	out := in[:0]
	var prev string
	for _, s := range in {
		if s == prev {
			continue
		}
		out = append(out, s)
		prev = s
	}
	return out
}

func formatCompleterSuffixes(word string, matches []string) ([][]rune, int) {
	if len(matches) == 0 {
		return nil, 0
	}
	wordRunes := []rune(word)
	wordLen := len(wordRunes)
	out := make([][]rune, 0, len(matches))
	for _, m := range matches {
		mRunes := []rune(m)
		if wordLen > len(mRunes) {
			continue
		}
		if !strings.EqualFold(string(mRunes[:wordLen]), word) {
			continue
		}
		out = append(out, mRunes[wordLen:])
	}
	if len(out) == 0 {
		return nil, 0
	}
	return out, wordLen
}
