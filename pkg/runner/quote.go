package runner

import "strings"

// Quote renders argv as a POSIX shell command line, single-quoting any token outside [A-Za-z0-9_@%+=:,./-].
func Quote(argv []string) string {
	tokens := make([]string, len(argv))
	for i, a := range argv {
		tokens[i] = quote(a)
	}
	return strings.Join(tokens, " ")
}

func quote(s string) string {
	if s != "" && strings.IndexFunc(s, unsafeRune) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func unsafeRune(r rune) bool {
	switch {
	case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z', '0' <= r && r <= '9':
		return false
	}
	return !strings.ContainsRune("_@%+=:,./-", r)
}
