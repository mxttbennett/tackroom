package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteMCPServerInEveryMainFormat(t *testing.T) {
	remote := mcpServerConfig{Name: "docs", Enabled: true, URL: "https://mcp.example.com/mcp", Headers: map[string]string{
		"Authorization": "Bearer ${DOCS_TOKEN}", "X-Region": "eu",
	}}
	cases := map[string][]string{
		agentClaudeCode: {`"type": "http"`, `"url": "https://mcp.example.com/mcp"`, `"Authorization": "Bearer ${DOCS_TOKEN}"`},
		agentCodex:      {`url = "https://mcp.example.com/mcp"`, `bearer_token_env_var = "DOCS_TOKEN"`, `http_headers = { "X-Region" = "eu" }`},
		agentPi:         {`"url": "https://mcp.example.com/mcp"`, `"Authorization": "Bearer ${DOCS_TOKEN}"`},
		agentGrok:       {`url = "https://mcp.example.com/mcp"`, `"Authorization" = "Bearer ${DOCS_TOKEN}"`},
		agentCursor:     {`"url": "https://mcp.example.com/mcp"`, `"Authorization": "Bearer ${env:DOCS_TOKEN}"`},
		agentGemini:     {`"httpUrl": "https://mcp.example.com/mcp"`, `"X-Region": "eu"`},
	}
	for agent, want := range cases {
		t.Run(agent, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("GROK_HOME", "")
			if err := patchMCPServer(agent, remote, home); err != nil {
				t.Fatal(err)
			}
			target, _ := mcpTargetForHarness(agent)
			data, _ := os.ReadFile(target.configPath(home))
			for _, w := range want {
				if !strings.Contains(string(data), w) {
					t.Fatalf("%s missing %s:\n%s", agent, w, data)
				}
			}
			if strings.Contains(string(data), `"command"`) || strings.Contains(string(data), "command =") {
				t.Fatalf("a remote server must not carry a command:\n%s", data)
			}
			if state, err := inspectMCPServer(agent, remote, home); err != nil || state != stateSynced {
				t.Fatalf("after patch: %s, %v\n%s", state, err, data)
			}
			if read, err := readNativeMCPServer(agent, "docs", home); err != nil || read.URL != remote.URL {
				t.Fatalf("import reads the url back: %#v, %v", read, err)
			}
		})
	}
}

func TestRemoteMCPKeepsARealHeaderValue(t *testing.T) {
	for _, agent := range []string{agentClaudeCode, agentCodex, agentGrok, agentPi} {
		t.Run(agent, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("GROK_HOME", "")
			real := mcpServerConfig{Name: "docs", Enabled: true, URL: "https://mcp.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer real-secret"}}
			if err := patchMCPServer(agent, real, home); err != nil {
				t.Fatal(err)
			}
			canonical := real
			canonical.Headers = map[string]string{"Authorization": "Bearer ${DOCS_TOKEN}"}
			if state, err := inspectMCPServer(agent, canonical, home); err != nil || state != stateSynced {
				t.Fatalf("a real value satisfies a reference: %s, %v", state, err)
			}
			if err := patchMCPServer(agent, canonical, home); err != nil {
				t.Fatal(err)
			}
			target, _ := mcpTargetForHarness(agent)
			if data, _ := os.ReadFile(target.configPath(home)); !strings.Contains(string(data), "real-secret") {
				t.Fatalf("the secret was replaced:\n%s", data)
			}
		})
	}
}

func TestRemoteMCPSkipsAgentsWithoutRemoteSupport(t *testing.T) {
	cfg := config{MCPServers: []mcpServerConfig{
		{Name: "docs", Enabled: true, URL: "https://mcp.example.com/mcp"},
		{Name: "local", Enabled: true, Command: "local-mcp"},
	}}
	if got := desiredMCPServersForAgent(cfg, agentOpenCode); len(got) != 1 || got[0].Name != "local" {
		t.Fatalf("opencode gets only stdio servers, got %#v", got)
	}
	if got := desiredMCPServersForAgent(cfg, agentCodex); len(got) != 2 {
		t.Fatalf("codex gets both, got %#v", got)
	}
}

func TestCodexRejectsAHeaderItCannotReadFromTheEnvironment(t *testing.T) {
	server := mcpServerConfig{Name: "docs", Enabled: true, URL: "https://mcp.example.com/mcp", Headers: map[string]string{"X-Key": "token ${KEY}"}}
	if err := patchMCPServer(agentCodex, server, t.TempDir()); err == nil || !strings.Contains(err.Error(), "Bearer ${NAME}") {
		t.Fatalf("want an actionable error, got %v", err)
	}
}

func TestMCPTransportValidation(t *testing.T) {
	for _, bad := range []mcpServerConfig{
		{Name: "a"},
		{Name: "a", Command: "x", URL: "https://x"},
		{Name: "a", URL: "ftp://x"},
		{Name: "a", URL: "https://x", Env: map[string]string{"K": "v"}},
		{Name: "a", Command: "x", Headers: map[string]string{"K": "v"}},
	} {
		if err := validateMCPTransport(bad); err == nil {
			t.Errorf("%#v should be rejected", bad)
		}
	}
	if err := validateMCPTransport(mcpServerConfig{Name: "a", URL: "https://x", Headers: map[string]string{"K": "v"}}); err != nil {
		t.Fatal(err)
	}
}

func TestMCPImportRedactsSecretHeaders(t *testing.T) {
	headers, redacted := redactImportedMCPHeaders("docs", map[string]string{
		"Authorization": "Bearer abc", "X-Api-Key": "k", "X-Region": "eu", "X-Token": "${env:TOK}",
	}, nil)
	if headers["Authorization"] != "Bearer ${DOCS_AUTHORIZATION}" || headers["X-Api-Key"] != "${DOCS_X_API_KEY}" || headers["X-Region"] != "eu" || headers["X-Token"] != "${TOK}" {
		t.Fatalf("headers = %#v", headers)
	}
	if len(redacted) != 2 {
		t.Fatalf("redacted = %v", redacted)
	}
}

func TestRestorePutsBackupsBackAndCanBeUndone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	backup := filepath.Join(conflictBackupRoot(home), "20261001T000000Z")
	writeSyncTestFile(t, filepath.Join(backup, ".claude", "CLAUDE.md"), []byte("# mine\n"))
	writeSyncTestFile(t, filepath.Join(backup, ".claude", "skills", "review", "SKILL.md"), []byte("old review\n"))
	writeSyncTestFile(t, filepath.Join(backup, ".codex", "config.toml"), []byte("original\n"))
	writeSyncTestFile(t, filepath.Join(backup, ".codex", "config.toml.1"), []byte("intermediate\n"))
	// The current state: tackroom linked the shared skill and edited Codex.
	canonical := filepath.Join(home, ".agents", "skills", "review")
	writeSyncTestFile(t, filepath.Join(canonical, "SKILL.md"), []byte("shared review\n"))
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, filepath.Join(home, ".claude", "skills", "review")); err != nil {
		t.Fatal(err)
	}
	writeSyncTestFile(t, filepath.Join(home, ".codex", "config.toml"), []byte("managed\n"))

	var out bytes.Buffer
	if err := restoreBackup([]string{"20261001T000000Z", "--dry-run"}, &out); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml")); string(data) != "managed\n" || !strings.Contains(out.String(), "3 paths would change") {
		t.Fatalf("dry run must not change anything:\n%s", out.String())
	}
	out.Reset()
	if err := restoreBackup([]string{"20261001T000000Z"}, &out); err != nil {
		t.Fatalf("restore: %v\n%s", err, out.String())
	}
	for path, want := range map[string]string{".claude/CLAUDE.md": "# mine\n", ".claude/skills/review/SKILL.md": "old review\n", ".codex/config.toml": "original\n"} {
		if data, _ := os.ReadFile(filepath.Join(home, path)); string(data) != want {
			t.Fatalf("%s = %q, want %q", path, data, want)
		}
	}
	if info, _ := os.Lstat(filepath.Join(home, ".claude", "skills", "review")); info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("the link should be replaced by the restored directory")
	}
	if data, _ := os.ReadFile(filepath.Join(canonical, "SKILL.md")); string(data) != "shared review\n" {
		t.Fatalf("restore wrote through the link into the shared repo: %q", data)
	}

	// The paths it replaced went into a new backup; restoring that undoes it.
	backups, _ := listBackups(conflictBackupRoot(home))
	if len(backups) != 2 || backups[1] != "20261001T000000Z" {
		t.Fatalf("backups = %v", backups)
	}
	out.Reset()
	if err := restoreBackup([]string{"latest"}, &out); err != nil {
		t.Fatalf("undo: %v\n%s", err, out.String())
	}
	if data, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml")); string(data) != "managed\n" {
		t.Fatalf("undo should bring the managed config back, got %q", data)
	}
	if target, err := os.Readlink(filepath.Join(home, ".claude", "skills", "review")); err != nil || target != canonical {
		t.Fatalf("undo should bring the link back: %q %v", target, err)
	}
}

func TestRestoreListsBackups(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	var out bytes.Buffer
	if err := restoreBackup(nil, &out); err != nil || !strings.Contains(out.String(), "No backups") {
		t.Fatalf("empty: %v\n%s", err, out.String())
	}
	writeSyncTestFile(t, filepath.Join(conflictBackupRoot(home), "20261002T000000Z", ".codex", "config.toml"), []byte("x"))
	out.Reset()
	if err := restoreBackup(nil, &out); err != nil || !strings.Contains(out.String(), "20261002T000000Z  ~/.codex/config.toml") {
		t.Fatalf("list: %v\n%s", err, out.String())
	}
	if err := restoreBackup([]string{"../etc"}, &out); err == nil {
		t.Fatal("an unknown backup id must be refused")
	}
}

func TestMCPAddRemoteServerFromTheCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".agents", "tackroom.yaml")
	writeMCPLifecycleConfig(t, filepath.Join(home, ".agents"), "  []\n")
	if err := runMCPAdd([]string{"docs", "--url", "https://mcp.example.com/mcp", "--header", "Authorization: Bearer ${DOCS_TOKEN}", "--config", configPath}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(configPath)
	for _, want := range []string{"url: https://mcp.example.com/mcp", "Authorization: Bearer ${DOCS_TOKEN}"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("tackroom.yaml missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "command:") {
		t.Fatalf("a remote server has no command:\n%s", data)
	}
	if err := runMCPAdd([]string{"bad", "--url", "https://x", "--command", "y", "--config", configPath}); err == nil {
		t.Fatal("--url with --command must be refused")
	}
}
