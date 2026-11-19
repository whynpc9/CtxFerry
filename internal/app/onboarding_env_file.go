package app

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

type envFile struct {
	body   []byte
	names  []string
	values map[string][]byte
}

func readEnvFile(path string) (envFile, error) {
	unreadable := Fail("invalid-env-file", "choose a readable regular UTF-8 env file")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return envFile{}, unreadable
	}
	f, err := os.Open(path)
	if err != nil {
		return envFile{}, unreadable
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return envFile{}, unreadable
	}
	body, err := io.ReadAll(io.LimitReader(f, BodyLimit+1))
	parsed := envFile{body: body, names: []string{}, values: make(map[string][]byte)}
	ok := false
	defer func() {
		if !ok {
			clear(body)
		}
	}()
	if err != nil || !utf8.Valid(body) || bytes.ContainsRune(body, 0) {
		return envFile{}, unreadable
	}
	if len(body) > BodyLimit {
		return envFile{}, Fail("size-limit", "env file exceeds 1 MiB")
	}
	seen := map[string]bool{}
	lineNumber := 0
	for line := range bytes.SplitSeq(bytes.TrimPrefix(body, []byte("\uFEFF")), []byte("\n")) {
		lineNumber++
		line = bytes.Trim(bytes.TrimSuffix(line, []byte("\r")), " \t")
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		if bytes.HasPrefix(line, []byte("export ")) || bytes.HasPrefix(line, []byte("export\t")) {
			line = bytes.TrimLeft(line[len("export"):], " \t")
		}
		key, value, assignment := bytes.Cut(line, []byte("="))
		key = bytes.TrimRight(key, " \t")
		if !assignment || len(key) > 128 || !environmentName.Match(key) {
			return envFile{}, invalidEnvLine(lineNumber)
		}
		value, literal := envLiteral(bytes.TrimLeft(value, " \t"))
		if !literal {
			return envFile{}, invalidEnvLine(lineNumber)
		}
		name := string(key)
		folded := strings.ToUpper(name)
		if seen[folded] {
			return envFile{}, Fail("duplicate-env-field", fmt.Sprintf("duplicate variable name on line %d", lineNumber))
		}
		seen[folded] = true
		parsed.names = append(parsed.names, name)
		parsed.values[name] = value
		if len(parsed.names) > 512 {
			return envFile{}, Fail("size-limit", "env file contains more than 512 variables")
		}
	}
	ok = true
	return parsed, nil
}

func invalidEnvLine(n int) error {
	return Fail("invalid-env-file", fmt.Sprintf("line %d is outside the literal env syntax; use single-line NAME=value assignments", n))
}

func envLiteral(raw []byte) ([]byte, bool) {
	if len(raw) == 0 || raw[0] == '#' {
		return raw[:0], true
	}
	value, tail := raw, []byte(nil)
	switch raw[0] {
	case '\'', '"':
		end := bytes.IndexByte(raw[1:], raw[0])
		if end < 0 {
			return nil, false
		}
		value, tail = raw[1:end+1], raw[end+2:]
		if raw[0] == '"' && bytes.ContainsAny(value, "$`\\") {
			return nil, false
		}
	default:
		if end := bytes.IndexAny(raw, " \t"); end >= 0 {
			value, tail = raw[:end], raw[end:]
		}
		if bytes.ContainsAny(value, "$`\\'\";&|<>()") || value[0] == '~' {
			return nil, false
		}
	}
	if len(tail) > 0 {
		comment := bytes.TrimLeft(tail, " \t")
		if len(comment) == len(tail) || len(comment) > 0 && comment[0] != '#' {
			return nil, false
		}
	}
	return value, bytes.IndexFunc(value, unicode.IsControl) < 0
}
