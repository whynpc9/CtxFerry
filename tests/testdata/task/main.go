package main

import (
	"encoding/json"
	"fmt"
	"os"
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
