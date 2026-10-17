package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionHookLifecycle(t *testing.T) {
	home := t.TempDir()
	hook := hookConfig{Name: "memory-session-start", Enabled: true, Event: "SessionStart", Command: `~/.agents/memory/hooks/session-start.sh "quoted"`, Timeout: 15}
	for _, tc := range []struct {
		agent string
		path  string
	}{
		{agentPi, filepath.Join(home, ".pi", "agent", "extensions", "tackroom-memory-session-start.ts")},
		{agentOMP, filepath.Join(home, ".omp", "agent", "hooks", "post", "tackroom-memory-session-start.ts")},
	} {
		target, ok := hookTargetForHarness(tc.agent)
		if !ok {
			t.Fatalf("%s has no hook target", tc.agent)
		}
		if state, err := target.inspect(hook, home); err != nil || state != stateMissing {
			t.Fatalf("%s before sync: %q, %v", tc.agent, state, err)
		}
		if err := target.patch(hook, home); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatalf("%s module not written: %v", tc.agent, err)
		}
		for _, want := range []string{
			`const command = "~/.agents/memory/hooks/session-start.sh \"quoted\"";`,
			`const timeoutMs = 15000;`,
			`pi.on("session_start",`,
			`agent: "` + tc.agent + `"`,
		} {
			if !strings.Contains(string(data), want) {
				t.Fatalf("%s module lacks %s:\n%s", tc.agent, want, data)
			}
		}
		if state, _ := target.inspect(hook, home); state != stateSynced {
			t.Fatalf("%s after sync: %q", tc.agent, state)
		}
		changed := hook
		changed.Timeout = 30
		if state, _ := target.inspect(changed, home); state != stateDrifted {
			t.Fatalf("%s with a new timeout: %q, want drifted", tc.agent, state)
		}

		entries, err := readExtensionHook(tc.agent, tc.path)
		if err != nil || len(entries) != 1 || entries[0].Command != hook.Command || entries[0].Event != "SessionStart" {
			t.Fatalf("%s hook list entry = %+v, %v", tc.agent, entries, err)
		}
		if !nativeHookIsManaged(entries[0], config{Hooks: []hookConfig{hook}}) {
			t.Fatalf("%s module not recognized as managed", tc.agent)
		}
		if _, err := removeNativeHookEntries(entries); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(tc.path); !os.IsNotExist(err) {
			t.Fatalf("%s module not removed: %v", tc.agent, err)
		}
	}
}

func TestExtensionHookUnsupportedEvents(t *testing.T) {
	home := t.TempDir()
	target, _ := hookTargetForHarness(agentOMP)
	for _, event := range []string{"Stop", "PreToolUse", "PostToolUse", "UserPromptSubmit"} {
		state, err := target.inspect(hookConfig{Name: "x", Enabled: true, Event: event, Command: "true"}, home)
		if err != nil || state != stateUnsupported {
			t.Fatalf("%s: %q, %v; want unsupported", event, state, err)
		}
	}
}

func TestExtensionHookDefaultTimeoutAndSafeFileName(t *testing.T) {
	hook := hookConfig{Name: "a/b c", Event: "SessionEnd", Command: "true"}
	data, ok := renderExtensionHook(agentPi, hook)
	if !ok || !strings.Contains(string(data), "const timeoutMs = 60000;") || !strings.Contains(string(data), `pi.on("session_shutdown",`) {
		t.Fatalf("render = %v:\n%s", ok, data)
	}
	if got := filepath.Base(extensionHookPath("/x", hook)); got != "tackroom-a-b-c.ts" {
		t.Fatalf("file name = %q", got)
	}
}
