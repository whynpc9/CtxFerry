package app

import (
	"bytes"
	"io"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestRedactorAcrossWrites(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		writes []string
		want   string
	}{
		{"single write", []string{"s3cret"}, []string{"a s3cret b"}, "a [REDACTED] b"},
		{"byte at a time", []string{"s3cret"}, strings.Split("xs3crets3cretx", ""), "x[REDACTED][REDACTED]x"},
		{"split value", []string{"s3cret"}, []string{"pre s3", "cr", "et post"}, "pre [REDACTED] post"},
		{"false prefix released", []string{"s3cret"}, []string{"s3c", "x"}, "s3cx"},
		{"held prefix flushed at end", []string{"s3cret"}, []string{"tail s3c"}, "tail s3c"},
		{"longest value first", []string{"abc", "abcdef"}, []string{"abcdef abc"}, "[REDACTED] [REDACTED]"},
		{"empty value ignored", []string{"", "k"}, []string{"kok"}, "[REDACTED]o[REDACTED]"},
		{"overlapping start", []string{"aab"}, []string{"a", "a", "a", "b"}, "a[REDACTED]"},
		{"contained value at EOF", []string{"prefix-TOKEN-suffix", "TOKEN"}, []string{"prefix-TOKEN"}, "prefix-[REDACTED]"},
		{"shorter value at EOF", []string{"abc", "abcdef"}, []string{"abc"}, "[REDACTED]"},
		{"longer value across writes", []string{"abc", "abcdef"}, []string{"abc", "def"}, "[REDACTED]"},
		{"longer value diverges", []string{"abc", "abcdef"}, []string{"abc", "dx"}, "[REDACTED]dx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			values := make([][]byte, 0, len(tc.values))
			for _, value := range tc.values {
				values = append(values, []byte(value))
			}
			r := newRedactor(&out, values)
			for _, write := range tc.writes {
				if n, err := r.Write([]byte(write)); err != nil || n != len(write) {
					t.Fatal(n, err)
				}
			}
			if err := r.Flush(); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("got %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestChildEnvironmentReplacesBaseNames(t *testing.T) {
	t.Setenv("HOME", "/caller")
	t.Setenv("GH_TOKEN", "caller")
	posix := runtime.GOOS != "windows"
	if posix {
		t.Setenv("path", "unrelated")
	}
	env := childEnvironment([]string{"home=/delivered", "DATABASE_URL=x"})
	if !slices.Contains(env, "home=/delivered") || !slices.Contains(env, "DATABASE_URL=x") ||
		slices.Contains(env, "HOME=/caller") != posix {
		t.Fatal(env)
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, "GH_TOKEN=") || strings.HasPrefix(entry, "path=") {
			t.Fatal("unrelated or authentication material reached the child", env)
		}
	}
	// The credential-free base keeps its previous case-folded selection.
	if base := taskEnvironment(); posix && !slices.Contains(base, "path=unrelated") || slices.ContainsFunc(base, func(e string) bool {
		return strings.HasPrefix(e, "GH_TOKEN=")
	}) {
		t.Fatal(base)
	}
}

func TestDeliverableValue(t *testing.T) {
	if deliverableValue([]byte("a\x00b")) || !deliverableValue([]byte("plain")) || !deliverableValue(nil) {
		t.Fatal("NUL handling")
	}
	if deliverableValue([]byte{0xff, 'x'}) != (runtime.GOOS != "windows") {
		t.Fatal("invalid UTF-8 must be refused only where the environment would alter it")
	}
}

func TestRedactorNilDestination(t *testing.T) {
	r := newRedactor(nil, [][]byte{[]byte("s3cret")})
	if n, err := r.Write([]byte("output s3cret")); err != nil || n != 13 || r.Flush() != nil {
		t.Fatal(n, err)
	}
}

func TestRedactorLongNearMatchIsLinear(t *testing.T) {
	value := append(bytes.Repeat([]byte("a"), 64*1024), 'b')
	var out bytes.Buffer
	r := newRedactor(&out, [][]byte{value})
	chunk := bytes.Repeat([]byte("a"), 32*1024)
	for range 128 { // 4 MiB of near-match; quadratic scanning would take minutes.
		if _, err := r.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	r.Write([]byte("b"))
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("a", 4*1024*1024-64*1024) + redactionMarker
	if out.String() != want {
		t.Fatal("near-match stream was not redacted exactly", out.Len())
	}
}

func BenchmarkRedactorNearMatch(b *testing.B) {
	value := append(bytes.Repeat([]byte("a"), 4096), 'b')
	chunk := bytes.Repeat([]byte("a"), 32*1024)
	b.SetBytes(int64(len(chunk)))
	r := newRedactor(io.Discard, [][]byte{value})
	for b.Loop() {
		r.Write(chunk)
	}
}
