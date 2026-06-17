package replcli

import (
	"strings"
)

// splitFields splits a REPL line into fields (whitespace-separated, single/double quotes).
func splitFields(line string) []string {
	var fields []string
	var cur strings.Builder
	inQuote := false
	var quote byte

	flush := func() {
		if cur.Len() > 0 {
			fields = append(fields, cur.String())
			cur.Reset()
		}
	}

	for i := 0; i < len(line); i++ {
		c := line[i]
		if inQuote {
			if c == quote {
				inQuote = false
			} else {
				cur.WriteByte(c)
			}
			continue
		}
		switch c {
		case '"', '\'':
			inQuote = true
			quote = c
		case ' ', '\t':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return fields
}
