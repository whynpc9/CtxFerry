package app

import (
	"bytes"
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
	env := childEnvironment([]string{"home=/delivered", "DATABASE_URL=x"})
	if slices.Contains(env, "HOME=/caller") || !slices.Contains(env, "home=/delivered") || !slices.Contains(env, "DATABASE_URL=x") {
		t.Fatal(env)
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, "GH_TOKEN=") {
			t.Fatal("authentication material reached the child", env)
		}
	}
}
