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
	wordStart, word := currentWordRunes(line, pos)
	prefix := strings.TrimSpace(string(line[:wordStart]))
	tokens := splitFields(prefix)
	candidates := c.candidates(tokens, word)
	suffixes, _ := formatCompleterSuffixes(word, candidates)
	return suffixes, pos - wordStart
}

func newReplCompleter(s *Session) readline.AutoCompleter {
	return &replCompleter{session: s}
}

func currentWordRunes(line []rune, pos int) (start int, word string) {
	start = pos
	for start > 0 && line[start-1] != ' ' && line[start-1] != '\t' {
		start--
	}
	return start, string(line[start:pos])
}

func currentWord(line []rune, pos int) (start int, word string) {
	return currentWordRunes(line, pos)
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
		if cmd == "apply-update" && completingBundlePath(tokens) {
			return filePathCandidates(word)
		}
	case "apply-update-all", "apply-update-all-remotes":
		if completingApplyUpdateAllBundle(tokens) {
			return filePathCandidates(word)
		}
	}
	return nil
}

// completingBundlePath reports whether tokens name apply-update with target set and bundle path being typed.
func completingBundlePath(tokens []string) bool {
	if len(tokens) == 0 || !strings.EqualFold(tokens[0], "apply-update") {
		return false
	}
	return len(stripApplyUpdateFlags(tokens)) == 1
}

func completingApplyUpdateAllBundle(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	cmd := strings.ToLower(tokens[0])
	if cmd != "apply-update-all" && cmd != "apply-update-all-remotes" {
		return false
	}
	return len(stripApplyUpdateFlags(tokens)) == 0
}

func stripApplyUpdateFlags(tokens []string) []string {
	var out []string
	for i := 1; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case t == "-use-bundle-config" || t == "--use-bundle-config":
		case strings.HasPrefix(t, "-agent-variant="):
		case t == "-agent-variant" && i+1 < len(tokens):
			i++
		case strings.HasPrefix(t, "-apiprefix="):
		default:
			out = append(out, t)
		}
	}
	return out
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
	dir, base, pathPrefix := filePathCompletionContext(partial)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	showHidden := strings.HasPrefix(base, ".") || strings.Contains(partial, "/.")
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if base != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(base)) {
			continue
		}
		full := pathPrefix + name
		if e.IsDir() {
			full += string(filepath.Separator)
		}
		if partial != "" && !strings.HasPrefix(strings.ToLower(full), strings.ToLower(partial)) {
			continue
		}
		out = append(out, full)
	}
	sort.Strings(out)
	return out
}

func filePathCompletionContext(partial string) (dir, base, pathPrefix string) {
	partial = strings.TrimSpace(partial)
	if partial == "" {
		return ".", "", ""
	}

	fsPartial := partial
	if partial == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ".", partial, ""
		}
		return home, "", "~/"
	}
	if strings.HasPrefix(partial, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ".", partial, ""
		}
		fsPartial = filepath.Join(home, partial[2:])
	}

	if strings.HasSuffix(partial, "/") || strings.HasSuffix(partial, string(filepath.Separator)) {
		dir := filepath.Clean(strings.TrimRight(fsPartial, string(filepath.Separator)))
		if dir == "" {
			dir = "."
		}
		return dir, "", partial
	}

	dir = filepath.Dir(fsPartial)
	base = filepath.Base(fsPartial)
	if dir == "" {
		dir = "."
	}
	dir = filepath.Clean(dir)

	if base != "" && strings.HasSuffix(partial, base) {
		pathPrefix = partial[:len(partial)-len(base)]
	} else {
		pathPrefix = partial
	}
	return dir, base, pathPrefix
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
	wordLower := strings.ToLower(word)
	for _, m := range matches {
		if word != "" && !strings.HasPrefix(strings.ToLower(m), wordLower) {
			continue
		}
		mRunes := []rune(m)
		if len(mRunes) < wordLen {
			continue
		}
		out = append(out, mRunes[wordLen:])
	}
	if len(out) == 0 {
		return nil, 0
	}
	return out, wordLen
}
