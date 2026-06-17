package replcli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"contrabass-agent/maintenance/appmeta"

	"github.com/chzyer/readline"
	"github.com/mattn/go-isatty"
)

type replReader interface {
	ReadLine() (string, error)
	Close() error
}

type plainReplReader struct {
	prompt  string
	scanner *bufio.Scanner
}

func (r *plainReplReader) ReadLine() (string, error) {
	fmt.Print(r.prompt)
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return r.scanner.Text(), nil
}

func (r *plainReplReader) Close() error { return nil }

type ttyReplReader struct {
	rl *readline.Instance
}

func (r *ttyReplReader) ReadLine() (string, error) {
	return r.rl.Readline()
}

func (r *ttyReplReader) Close() error {
	return r.rl.Close()
}

func newREPLReader(prompt string, s *Session) (replReader, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return &plainReplReader{
			prompt:  prompt,
			scanner: bufio.NewScanner(os.Stdin),
		}, nil
	}
	cfg := &readline.Config{
		Prompt:       prompt,
		AutoComplete: newReplCompleter(s),
	}
	if path := replHistoryPath(); path != "" {
		cfg.HistoryFile = path
	}
	rl, err := readline.NewEx(cfg)
	if err != nil {
		return nil, err
	}
	return &ttyReplReader{rl: rl}, nil
}

func replHistoryPath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, appmeta.BinaryName, "repl_history")
}
