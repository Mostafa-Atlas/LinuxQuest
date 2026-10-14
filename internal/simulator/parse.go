package simulator

import (
	"errors"
	"strings"
	"unicode"
)

// MaxLineLen bounds how much input the simulator will even look at.
const MaxLineLen = 1024

var (
	ErrTooLong      = errors.New("input line is too long")
	ErrUnterminated = errors.New("unterminated quote")
	ErrShellSyntax  = errors.New("pipes, redirects, and command chaining are not supported in this simulator")
)

// Parse splits a command line into words. It understands single quotes,
// double quotes and backslash escapes, and nothing else: there is no
// variable expansion, globbing, substitution, or chaining. Unquoted shell
// operators are rejected rather than interpreted. The result is only ever used
// as data for the in-memory simulator and is never given to a shell.
func Parse(line string) ([]string, error) {
	if len(line) > MaxLineLen {
		return nil, ErrTooLong
	}
	var (
		words   []string
		cur     strings.Builder
		inWord  bool
		quote   rune
		escaped bool
	)
	flush := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for _, r := range line {
		// Never let control characters (e.g. terminal escape sequences) into
		// the parsed words, quoted or not: they would be echoed back in output.
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			continue
		}
		switch {
		case escaped:
			cur.WriteRune(r)
			inWord = true
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped = true
			inWord = true
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case strings.ContainsRune(";&|<>", r):
			return nil, ErrShellSyntax
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, ErrUnterminated
	}
	flush()
	return words, nil
}
