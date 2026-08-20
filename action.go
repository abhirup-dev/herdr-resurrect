package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// action is the plugin entrypoint dispatched from herdr-plugin.toml
// [[actions]]. The host injects HERDR_PLUGIN_* env vars; HERDR_PLUGIN_CONTEXT_JSON
// carries invocation context.
func action(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: herdr-resurrect action <id>")
		return 2
	}
	switch args[0] {
	case "ping":
		return actionPing()
	default:
		fmt.Fprintf(os.Stderr, "unknown action %q\n", args[0])
		return 2
	}
}

func actionPing() int {
	keys := make([]string, 0)
	for _, kv := range os.Environ() {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if strings.HasPrefix(k, "HERDR_") || strings.HasPrefix(k, "HERDR_PLUGIN_") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	fmt.Println("herdr-resurrect pong")
	if ctx := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"); ctx != "" {
		var pretty map[string]any
		if err := json.Unmarshal([]byte(ctx), &pretty); err == nil {
			if b, err := json.MarshalIndent(pretty, "", "  "); err == nil {
				ctx = string(b)
			}
		}
		fmt.Printf("context: %s\n", ctx)
	}
	for _, k := range keys {
		fmt.Printf("%s=%s\n", k, os.Getenv(k))
	}
	return 0
}
