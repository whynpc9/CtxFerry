package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

func main() {
	for _, key := range []string{"GH_TOKEN", "BW_SESSION", "GIT_CONFIG_COUNT", "CTXFERRY_TEST_API",
		"INFISICAL_TOKEN", "INFISICAL_CLIENT_ID", "INFISICAL_CLIENT_SECRET", "CTXFERRY_INFISICAL_ACCESS_TOKEN"} {
		if _, exists := os.LookupEnv(key); exists {
			fmt.Fprintln(os.Stderr, "unexpected inherited variable", key)
			os.Exit(98)
		}
	}
	switch os.Args[1] {
	case "credentials":
		// Report variable names and value digests, never the values.
		report := struct {
			Names   []string          `json:"names"`
			Digests map[string]string `json:"digests"`
		}{Digests: map[string]string{}}
		for _, entry := range os.Environ() {
			name, _, _ := strings.Cut(entry, "=")
			report.Names = append(report.Names, name)
		}
		slices.Sort(report.Names)
		for _, name := range os.Args[2:] {
			if value, ok := os.LookupEnv(name); ok {
				sum := sha256.Sum256([]byte(value))
				report.Digests[name] = hex.EncodeToString(sum[:])
			}
		}
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			panic(err)
		}
		os.Exit(0)
	case "leak":
		// A hostile program prints what it was given, split across writes.
		value := os.Getenv(os.Args[2])
		half := len(value) / 2
		os.Stdout.WriteString("before " + value[:half])
		os.Stdout.Sync()
		os.Stdout.WriteString(value[half:] + " after " + value + value + "\n")
		os.Stderr.WriteString("diagnostic " + value + "\n")
		os.Exit(5)
	case "marker":
		if err := os.WriteFile("task-ran", nil, 0600); err != nil {
			panic(err)
		}
	case "flood":
		fmt.Fprint(os.Stdout, strings.Repeat("<\x00", 100000))
		fmt.Fprint(os.Stderr, strings.Repeat("noise", 20000))
		os.Exit(23)
	case "inspect":
		data, err := os.ReadFile("fixture-location")
		if err != nil {
			panic(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(append([]string{string(data)}, os.Args[2:]...)); err != nil {
			panic(err)
		}
		fmt.Fprintln(os.Stderr, "child diagnostic")
		os.Exit(17)
	}
}
