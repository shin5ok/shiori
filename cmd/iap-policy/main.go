package main

import (
	"fmt"
	"os"

	"my-bookmark/internal/configfile"
	"my-bookmark/internal/iap"
)

func main() {
	if len(os.Args) != 4 && !(len(os.Args) == 3 && os.Args[1] == "--validate") {
		fmt.Fprintln(os.Stderr, "usage: iap-policy --validate CONFIG_YAML | CONFIG_YAML EXISTING_POLICY_JSON OUTPUT_POLICY_JSON")
		os.Exit(2)
	}
	allowPath := os.Args[1]
	if len(os.Args) == 3 {
		allowPath = os.Args[2]
	}
	settings, err := configfile.Load(allowPath)
	if err != nil {
		fail(err)
	}
	list := settings.Allowlist
	if len(os.Args) == 3 {
		return
	}
	current, err := os.ReadFile(os.Args[2])
	if err != nil {
		fail(err)
	}
	updated, err := iap.ReconcilePolicy(current, list)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(os.Args[3], updated, 0o600); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
