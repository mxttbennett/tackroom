package app

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Pi and OMP run TypeScript extension modules, not shell hooks. Each managed
// hook becomes one generated module that runs the hook command with a Claude
// Code-style JSON payload. Only events with a plain run-and-continue meaning
// map: Stop and the tool events need block/continue semantics the bridge does
// not reproduce.

//go:embed extension_hook.ts
var extensionHookTemplate string

var extensionHookEvents = map[string]string{
	"SessionStart": "session_start",
	"SessionEnd":   "session_shutdown",
}

// extensionHookDefaultTimeout matches Claude Code's default hook timeout.
const extensionHookDefaultTimeout = 60

const extensionHookPrefix = "tackroom-"

func piExtensionHookDir(home string) string {
	return filepath.Join(home, ".pi", "agent", "extensions")
}

// ompExtensionHookDir is a hook discovery directory; pre and post only group
// files, so either works for session events.
func ompExtensionHookDir(home string) string {
	return filepath.Join(home, ".omp", "agent", "hooks", "post")
}

func extensionHookTarget(agent string, dir func(home string) string) *hookTarget {
	return &hookTarget{
		agentName: agent,
		inspect: func(hook hookConfig, home string) (string, error) {
			return inspectExtensionHook(agent, dir(home), hook)
		},
		patch: func(hook hookConfig, home string) error {
			return patchExtensionHook(agent, dir(home), hook)
		},
	}
}

var unsafeHookFileChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func extensionHookPath(dir string, hook hookConfig) string {
	return filepath.Join(dir, extensionHookPrefix+unsafeHookFileChars.ReplaceAllString(hook.Name, "-")+".ts")
}

func renderExtensionHook(agent string, hook hookConfig) ([]byte, bool) {
	event, ok := extensionHookEvents[hook.Event]
	if !ok {
		return nil, false
	}
	timeout := hook.Timeout
	if timeout <= 0 {
		timeout = extensionHookDefaultTimeout
	}
	quote := func(s string) string {
		data, _ := json.Marshal(s)
		return string(data)
	}
	out := strings.NewReplacer(
		"__NAME__", quote(hook.Name),
		"__NATIVE_EVENT__", quote(event),
		"__HOOK_EVENT__", quote(hook.Event),
		"__COMMAND__", quote(hook.Command),
		"__TIMEOUT_MS__", strconv.Itoa(timeout*1000),
		"__AGENT__", quote(agent),
	).Replace(extensionHookTemplate)
	return []byte(out), true
}

func inspectExtensionHook(agent string, dir string, hook hookConfig) (string, error) {
	want, ok := renderExtensionHook(agent, hook)
	if !ok {
		return stateUnsupported, nil
	}
	path := extensionHookPath(dir, hook)
	got, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return stateMissing, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.Equal(got, want) {
		return stateSynced, nil
	}
	return stateDrifted, nil
}

func patchExtensionHook(agent string, dir string, hook hookConfig) error {
	data, ok := renderExtensionHook(agent, hook)
	if !ok {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	path := extensionHookPath(dir, hook)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// extensionHookFiles lists the generated hook modules in dir.
func extensionHookFiles(dir string) []string {
	paths, _ := filepath.Glob(filepath.Join(dir, extensionHookPrefix+"*.ts"))
	return paths
}

var extensionHookConst = regexp.MustCompile(`(?m)^const (command|hookEvent) = (".*");$`)

// readExtensionHook reads the command and event back from a generated module,
// for `tackroom hook list` and `hook remove`.
func readExtensionHook(agent string, path string) ([]nativeHookEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	entry := nativeHookEntry{Agent: agent, ConfigPath: path}
	for _, match := range extensionHookConst.FindAllStringSubmatch(string(data), -1) {
		var value string
		if err := json.Unmarshal([]byte(match[2]), &value); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if match[1] == "command" {
			entry.Command = value
		} else {
			entry.Event = value
		}
	}
	if strings.TrimSpace(entry.Command) == "" {
		return nil, nil
	}
	return []nativeHookEntry{entry}, nil
}
