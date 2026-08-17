// Package herdr is a thin client over the herdr CLI. The entire CLI is the
// plugin API; prefer HERDR_BIN_PATH (injected by the herdr plugin host) so
// the same code works when invoked as a plugin action or as a plain CLI.
package herdr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// Bin returns the herdr binary path to invoke.
func Bin() string {
	if p := os.Getenv("HERDR_BIN_PATH"); p != "" {
		return p
	}
	return "herdr"
}

// SessionScope returns argv that targets the named session ("" = default
// selection via env/socket). --session outranks HERDR_SOCKET_PATH.
func SessionScope(session string) []string {
	if session == "" {
		return nil
	}
	return []string{"--session", session}
}

// Run executes a herdr command and returns its stdout. Server errors come
// back as JSON on stderr with exit 1; syntax errors exit 2 with no JSON.
func Run(args ...string) ([]byte, error) {
	cmd := exec.Command(Bin(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("herdr %v: %w (stderr: %s)", args, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// RunRawInto executes a herdr command and decodes its stdout directly into
// v. Use for the few commands that emit bare JSON instead of the
// id/result envelope: `session list --json`, `api schema`, `plugin list`.
func RunRawInto(v any, args ...string) error {
	out, err := Run(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("herdr %v: decode: %w", args, err)
	}
	return nil
}

// RunInto executes a herdr command and decodes its `result` object into v.
func RunInto(v any, args ...string) error {
	out, err := Run(args...)
	if err != nil {
		return err
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return fmt.Errorf("herdr %v: decode: %w", args, err)
	}
	if err := json.Unmarshal(env.Result, v); err != nil {
		return fmt.Errorf("herdr %v: result decode: %w", args, err)
	}
	return nil
}
