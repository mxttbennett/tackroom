package app

import (
	"os"
	"path/filepath"
	"testing"
)

// droidRootInstructions returns the rootInstructionsCapability for the droid harness.
// Used by tests to exercise inspectRootInstructions with droid-specific paths.
func droidRootInstructions() *rootInstructionsCapability {
	h := harnessFor(agentDroid)
	if h == nil || h.RootInstructions == nil {
		panic("droid harness missing RootInstructions")
	}
	return h.RootInstructions
}

// writeSharedAgentsMD creates the canonical AGENTS.md; root instructions are
// only managed once it exists.
func writeSharedAgentsMD(t *testing.T, home string) {
	t.Helper()
	writeSyncTestFile(t, filepath.Join(home, ".agents", "AGENTS.md"), []byte("# Shared\n"))
}

func TestInspectRootInstructionsUnmanagedWithoutSharedFile(t *testing.T) {
	home := t.TempDir()
	writeSyncTestFile(t, filepath.Join(home, ".factory", "AGENTS.md"), []byte("# Local\n"))
	report := agentReport{}
	if err := inspectRootInstructions(&report, droidRootInstructions(), filepath.Join(home, ".agents"), home, nil); err != nil {
		t.Fatal(err)
	}
	if report.RootState != "" || report.RootPath != "" || len(report.Conflicts) != 0 {
		t.Fatalf("without ~/.agents/AGENTS.md the native file is left alone: %+v", report)
	}
}

func TestInspectRootInstructionsIdenticalCopyIsRelinked(t *testing.T) {
	home := t.TempDir()
	writeSharedAgentsMD(t, home)
	writeSyncTestFile(t, filepath.Join(home, ".factory", "AGENTS.md"), []byte("# Shared\n"))
	report := agentReport{}
	if err := inspectRootInstructions(&report, droidRootInstructions(), filepath.Join(home, ".agents"), home, nil); err != nil {
		t.Fatal(err)
	}
	if report.RootState != stateDrifted || len(report.Conflicts) != 0 {
		t.Fatalf("an identical copy should be relinked, not a conflict: %+v", report)
	}
}

func TestInspectDroidRootInstructionsMissing(t *testing.T) {
	home := t.TempDir()
	writeSharedAgentsMD(t, home)
	report := agentReport{}

	if err := inspectRootInstructions(&report, droidRootInstructions(), filepath.Join(home, ".agents"), home, nil); err != nil {
		t.Fatal(err)
	}

	if report.RootState != stateMissing {
		t.Fatalf("RootState = %q, want %q", report.RootState, stateMissing)
	}
	if report.RootExpected != filepath.Join(home, ".agents", "AGENTS.md") {
		t.Fatalf("RootExpected = %q", report.RootExpected)
	}
}

func TestInspectDroidRootInstructionsSynced(t *testing.T) {
	home := t.TempDir()
	expected := filepath.Join(home, ".agents", "AGENTS.md")
	linkPath := filepath.Join(home, ".factory", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(expected), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(expected, []byte("# Shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(expected, linkPath); err != nil {
		t.Fatal(err)
	}

	report := agentReport{}
	if err := inspectRootInstructions(&report, droidRootInstructions(), filepath.Join(home, ".agents"), home, nil); err != nil {
		t.Fatal(err)
	}

	if report.RootState != stateSynced {
		t.Fatalf("RootState = %q, want %q", report.RootState, stateSynced)
	}
}

func TestInspectDroidRootInstructionsConflict(t *testing.T) {
	home := t.TempDir()
	writeSharedAgentsMD(t, home)
	linkPath := filepath.Join(home, ".factory", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(linkPath, []byte("# Local\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report := agentReport{}
	if err := inspectRootInstructions(&report, droidRootInstructions(), filepath.Join(home, ".agents"), home, nil); err != nil {
		t.Fatal(err)
	}

	if report.RootState != stateConflict {
		t.Fatalf("RootState = %q, want %q", report.RootState, stateConflict)
	}
	if len(report.Conflicts) != 1 {
		t.Fatalf("Conflicts = %#v, want one conflict", report.Conflicts)
	}
}

func TestApplyAgentRootInstructionSyncCreatesMissingLink(t *testing.T) {
	home := t.TempDir()
	report := agentReport{
		Name:         agentDroid,
		Detected:     true,
		RootPath:     filepath.Join(home, ".factory", "AGENTS.md"),
		RootExpected: filepath.Join(home, ".agents", "AGENTS.md"),
		RootState:    stateMissing,
	}
	if err := os.MkdirAll(filepath.Dir(report.RootExpected), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report.RootExpected, []byte("# Shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := applyAgentRootInstructionSync([]agentReport{report}); err != nil {
		t.Fatal(err)
	}

	rawTarget, err := os.Readlink(report.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	if rawTarget != report.RootExpected {
		t.Fatalf("link target = %q, want %q", rawTarget, report.RootExpected)
	}
}

func claudeCodeRootInstructions() *rootInstructionsCapability {
	h := harnessFor(agentClaudeCode)
	if h == nil || h.RootInstructions == nil {
		panic("claude harness missing RootInstructions")
	}
	return h.RootInstructions
}

func codexRootInstructions() *rootInstructionsCapability {
	h := harnessFor(agentCodex)
	if h == nil || h.RootInstructions == nil {
		panic("codex harness missing RootInstructions")
	}
	return h.RootInstructions
}

func ampRootInstructions() *rootInstructionsCapability {
	h := harnessFor("amp")
	if h == nil || h.RootInstructions == nil {
		panic("amp harness missing RootInstructions")
	}
	return h.RootInstructions
}

func TestRootInstructionsNativePaths(t *testing.T) {
	home := t.TempDir()
	repoRoot := filepath.Join(home, ".agents")
	for _, tc := range []struct {
		agent    string
		cap      *rootInstructionsCapability
		wantLink string
	}{
		{"amp", ampRootInstructions(), filepath.Join(home, ".config", "amp", "AGENTS.md")},
		{agentClaudeCode, claudeCodeRootInstructions(), filepath.Join(home, ".claude", "CLAUDE.md")},
		{agentCodex, codexRootInstructions(), filepath.Join(home, ".codex", "AGENTS.md")},
	} {
		if got := tc.cap.Path(home); got != tc.wantLink {
			t.Fatalf("%s Path = %q, want %q", tc.agent, got, tc.wantLink)
		}
		if got := tc.cap.Expected(repoRoot); got != filepath.Join(repoRoot, "AGENTS.md") {
			t.Fatalf("%s Expected = %q, want %q", tc.agent, got, filepath.Join(repoRoot, "AGENTS.md"))
		}
	}
}

func TestInspectClaudeAndCodexRootInstructions(t *testing.T) {
	for _, tc := range []struct {
		agent string
		cap   func() *rootInstructionsCapability
		dir   string
		file  string
	}{
		{"amp", ampRootInstructions, ".config/amp", "AGENTS.md"},
		{agentClaudeCode, claudeCodeRootInstructions, ".claude", "CLAUDE.md"},
		{agentCodex, codexRootInstructions, ".codex", "AGENTS.md"},
	} {
		t.Run(tc.agent+"/missing", func(t *testing.T) {
			home := t.TempDir()
			writeSharedAgentsMD(t, home)
			report := agentReport{}
			if err := inspectRootInstructions(&report, tc.cap(), filepath.Join(home, ".agents"), home, nil); err != nil {
				t.Fatal(err)
			}
			if report.RootState != stateMissing {
				t.Fatalf("RootState = %q, want %q", report.RootState, stateMissing)
			}
		})
		t.Run(tc.agent+"/synced", func(t *testing.T) {
			home := t.TempDir()
			expected := filepath.Join(home, ".agents", "AGENTS.md")
			linkPath := filepath.Join(home, tc.dir, tc.file)
			if err := os.MkdirAll(filepath.Dir(expected), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(expected, []byte("# Shared\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(expected, linkPath); err != nil {
				t.Fatal(err)
			}
			report := agentReport{}
			if err := inspectRootInstructions(&report, tc.cap(), filepath.Join(home, ".agents"), home, nil); err != nil {
				t.Fatal(err)
			}
			if report.RootState != stateSynced {
				t.Fatalf("RootState = %q, want %q", report.RootState, stateSynced)
			}
		})
		t.Run(tc.agent+"/conflict", func(t *testing.T) {
			home := t.TempDir()
			writeSharedAgentsMD(t, home)
			linkPath := filepath.Join(home, tc.dir, tc.file)
			if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(linkPath, []byte("# Local\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			report := agentReport{}
			if err := inspectRootInstructions(&report, tc.cap(), filepath.Join(home, ".agents"), home, nil); err != nil {
				t.Fatal(err)
			}
			if report.RootState != stateConflict {
				t.Fatalf("RootState = %q, want %q", report.RootState, stateConflict)
			}
			if len(report.Conflicts) != 1 {
				t.Fatalf("Conflicts = %#v, want one conflict", report.Conflicts)
			}
		})
	}
}

func TestInspectAmpAgentWiresRootInstructions(t *testing.T) {
	home := t.TempDir()
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "AGENTS.md"), []byte("# Shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := agentConfig{
		Name:      "amp",
		Enabled:   true,
		SkillRoot: filepath.Join(home, ".agents", "skills"),
	}
	agentsSkillRoot := filepath.Join(repoRoot, "skills")

	report, err := inspectAmpAgent(agent, map[string]string{}, agentsSkillRoot, config{}, home)
	if err != nil {
		t.Fatal(err)
	}

	wantLink := filepath.Join(home, ".config", "amp", "AGENTS.md")
	wantTarget := filepath.Join(repoRoot, "AGENTS.md")
	if report.RootPath != wantLink {
		t.Fatalf("RootPath = %q, want %q", report.RootPath, wantLink)
	}
	if report.RootExpected != wantTarget {
		t.Fatalf("RootExpected = %q, want %q", report.RootExpected, wantTarget)
	}
	if report.RootState != stateMissing {
		t.Fatalf("RootState = %q, want %q", report.RootState, stateMissing)
	}

	if err := applyAgentRootInstructionSync([]agentReport{report}); err != nil {
		t.Fatal(err)
	}
	rawTarget, err := os.Readlink(wantLink)
	if err != nil {
		t.Fatalf("root instructions symlink not created: %v", err)
	}
	if rawTarget != wantTarget {
		t.Fatalf("link target = %q, want %q", rawTarget, wantTarget)
	}
}

func TestApplyAgentRootInstructionSyncClaudeAndCodex(t *testing.T) {
	for _, tc := range []struct {
		agent    string
		linkPath string
	}{
		{"amp", ".config/amp/AGENTS.md"},
		{agentClaudeCode, ".claude/CLAUDE.md"},
		{agentCodex, ".codex/AGENTS.md"},
	} {
		home := t.TempDir()
		report := agentReport{
			Name:         tc.agent,
			Detected:     true,
			RootPath:     filepath.Join(home, filepath.FromSlash(tc.linkPath)),
			RootExpected: filepath.Join(home, ".agents", "AGENTS.md"),
			RootState:    stateMissing,
		}
		if err := os.MkdirAll(filepath.Dir(report.RootExpected), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(report.RootExpected, []byte("# Shared\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := applyAgentRootInstructionSync([]agentReport{report}); err != nil {
			t.Fatal(err)
		}

		rawTarget, err := os.Readlink(report.RootPath)
		if err != nil {
			t.Fatal(err)
		}
		if rawTarget != report.RootExpected {
			t.Fatalf("%s link target = %q, want %q", tc.agent, rawTarget, report.RootExpected)
		}
	}
}

func rootInstructionsFor(t *testing.T, agent string) *rootInstructionsCapability {
	t.Helper()
	h := harnessFor(agent)
	if h == nil || h.RootInstructions == nil {
		t.Fatalf("%s harness missing RootInstructions", agent)
	}
	return h.RootInstructions
}

func TestRootInstructionsHermesAndOMPPaths(t *testing.T) {
	home := t.TempDir()
	if got, want := rootInstructionsFor(t, agentHermes).Path(home), filepath.Join(home, ".hermes", "SOUL.md"); got != want {
		t.Fatalf("hermes Path = %q, want %q", got, want)
	}
	if got, want := rootInstructionsFor(t, agentOMP).Path(home), filepath.Join(home, ".omp", "agent", "AGENTS.md"); got != want {
		t.Fatalf("omp Path = %q, want %q", got, want)
	}
}

// writeInstructionSources lays out AGENTS.md and SOUL.md in the config root
// and a profile outside it, returning the config root.
func writeInstructionSources(t *testing.T, home string) string {
	t.Helper()
	repoRoot := filepath.Join(home, ".agents")
	writeSyncTestFile(t, filepath.Join(repoRoot, "AGENTS.md"), []byte("# Shared\n"))
	writeSyncTestFile(t, filepath.Join(repoRoot, "SOUL.md"), []byte("# Soul\n"))
	writeSyncTestFile(t, filepath.Join(home, "vault", "USER.md"), []byte("# User\n"))
	return repoRoot
}

func inspectRootForTest(t *testing.T, agent string, repoRoot string, home string, instructions []string) agentReport {
	t.Helper()
	report := agentReport{Name: agent, Detected: true}
	if err := inspectRootInstructions(&report, rootInstructionsFor(t, agent), repoRoot, home, instructions); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestComposedRootInstructionsCopyLifecycle(t *testing.T) {
	home := t.TempDir()
	repoRoot := writeInstructionSources(t, home)
	instructions := []string{"SOUL.md", "~/vault/USER.md", "missing.md"}
	rootPath := filepath.Join(home, ".factory", "AGENTS.md")
	// Start from the plain link an earlier sync made.
	if err := os.MkdirAll(filepath.Dir(rootPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "AGENTS.md"), rootPath); err != nil {
		t.Fatal(err)
	}

	report := inspectRootForTest(t, agentDroid, repoRoot, home, instructions)
	if report.RootState != stateDrifted {
		t.Fatalf("a plain link with instructions configured is drifted, got %q", report.RootState)
	}
	if len(report.RootSources) != 3 || len(report.RootMissing) != 1 {
		t.Fatalf("sources = %v, missing = %v", report.RootSources, report.RootMissing)
	}
	if err := applyAgentRootInstructionSync([]agentReport{report}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "<!-- Generated by tackroom from ~/.agents/AGENTS.md, ~/.agents/SOUL.md, ~/vault/USER.md; do not edit directly. Edit the sources and run `tackroom sync`. -->\n\n# Shared\n\n# Soul\n\n# User\n"
	if string(got) != want {
		t.Fatalf("composed file:\n%s\nwant:\n%s", got, want)
	}
	if shared, _ := os.ReadFile(filepath.Join(repoRoot, "AGENTS.md")); string(shared) != "# Shared\n" {
		t.Fatalf("writing the composed file followed the old link into AGENTS.md: %q", shared)
	}
	if report := inspectRootForTest(t, agentDroid, repoRoot, home, instructions); report.RootState != stateSynced {
		t.Fatalf("after sync RootState = %q, want synced", report.RootState)
	}

	writeSyncTestFile(t, filepath.Join(repoRoot, "SOUL.md"), []byte("# Soul v2\n"))
	report = inspectRootForTest(t, agentDroid, repoRoot, home, instructions)
	if report.RootState != stateDrifted || len(report.Conflicts) != 0 {
		t.Fatalf("an edited source makes the copy stale, not a conflict: %+v", report)
	}

	// Dropping the instructions list turns the generated file back into a link.
	report = inspectRootForTest(t, agentDroid, repoRoot, home, nil)
	if report.RootState != stateDrifted || len(report.Conflicts) != 0 {
		t.Fatalf("a generated file without instructions is drifted, got %+v", report)
	}
	if err := applyAgentRootInstructionSync([]agentReport{report}); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(rootPath); err != nil || target != filepath.Join(repoRoot, "AGENTS.md") {
		t.Fatalf("root doc not relinked: %q, %v", target, err)
	}
}

func TestComposedRootInstructionsClaudeImports(t *testing.T) {
	home := t.TempDir()
	repoRoot := writeInstructionSources(t, home)
	instructions := []string{"SOUL.md", "~/vault/USER.md"}
	report := inspectRootForTest(t, agentClaudeCode, repoRoot, home, instructions)
	if report.RootState != stateMissing {
		t.Fatalf("RootState = %q, want missing", report.RootState)
	}
	if err := applyAgentRootInstructionSync([]agentReport{report}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "<!-- Generated by tackroom from ~/.agents/AGENTS.md, ~/.agents/SOUL.md, ~/vault/USER.md; do not edit directly. Edit the sources and run `tackroom sync`. -->\n@~/.agents/AGENTS.md\n@~/.agents/SOUL.md\n@~/vault/USER.md\n"
	if string(got) != want {
		t.Fatalf("CLAUDE.md:\n%s\nwant:\n%s", got, want)
	}
	// Imports stay live: editing a source needs no sync.
	writeSyncTestFile(t, filepath.Join(repoRoot, "SOUL.md"), []byte("# Soul v2\n"))
	if report := inspectRootForTest(t, agentClaudeCode, repoRoot, home, instructions); report.RootState != stateSynced {
		t.Fatalf("RootState = %q, want synced", report.RootState)
	}
}

func TestComposedRootInstructionsUserFileConflicts(t *testing.T) {
	home := t.TempDir()
	repoRoot := writeInstructionSources(t, home)
	writeSyncTestFile(t, filepath.Join(home, ".codex", "AGENTS.md"), []byte("# Local\n"))
	report := inspectRootForTest(t, agentCodex, repoRoot, home, []string{"SOUL.md"})
	if report.RootState != stateConflict || len(report.Replaceable) != 1 {
		t.Fatalf("a file the user wrote is a replaceable conflict: %+v", report)
	}
}

func TestInspectHermesAgentWiresRootInstructions(t *testing.T) {
	home := t.TempDir()
	// Hermes's skills-dir check resolves $HOME itself.
	t.Setenv("HOME", home)
	writeSyncTestFile(t, filepath.Join(home, ".hermes", "config.yaml"), []byte("model: {}\n"))
	repoRoot := writeInstructionSources(t, home)
	agent := agentConfig{Name: agentHermes, Enabled: true, SkillRoot: filepath.Join(home, ".hermes", "skills")}
	cfg := config{Instructions: []string{"SOUL.md"}}
	report, err := inspectHermesAgent(agent, map[string]string{}, filepath.Join(repoRoot, "skills"), cfg, home)
	if err != nil {
		t.Fatal(err)
	}
	if report.RootPath != filepath.Join(home, ".hermes", "SOUL.md") || report.RootState != stateMissing || len(report.RootSources) != 2 {
		t.Fatalf("hermes root doc not inspected: %+v", report)
	}
}

func TestRootInstructionSourcesSkipsHermesSeed(t *testing.T) {
	home := t.TempDir()
	repoRoot := filepath.Join(home, ".agents")
	writeSyncTestFile(t, filepath.Join(home, ".hermes", "SOUL.md"), []byte("You are Hermes Agent.\n"))
	writeSyncTestFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), []byte("# Mine\n"))
	detected := []agentConfig{{Name: agentHermes}, {Name: agentClaudeCode}}
	sources := rootInstructionSources(repoRoot, detected, home)
	if len(sources) != 1 || sources[0].Agent != agentClaudeCode {
		t.Fatalf("setup should import only Claude's file, got %+v", sources)
	}
}

func TestConfigInstructionsValidateAndMerge(t *testing.T) {
	cfg := config{Instructions: []string{" SOUL.md "}}
	if err := validateConfig(&cfg, t.TempDir(), true); err != nil {
		t.Fatal(err)
	}
	if cfg.Instructions[0] != "SOUL.md" {
		t.Fatalf("instructions not trimmed: %q", cfg.Instructions)
	}
	for _, bad := range [][]string{{"AGENTS.md"}, {"SOUL.md", "SOUL.md"}, {" "}} {
		if err := validateConfig(&config{Instructions: bad}, t.TempDir(), true); err == nil {
			t.Fatalf("instructions %q should be rejected", bad)
		}
	}
	base := config{Instructions: []string{"SOUL.md"}}
	mergeConfig(&base, config{})
	if len(base.Instructions) != 1 {
		t.Fatalf("an overlay without instructions keeps the shared list: %q", base.Instructions)
	}
	mergeConfig(&base, config{Instructions: []string{}})
	if len(base.Instructions) != 0 {
		t.Fatalf("an empty overlay list clears the shared one: %q", base.Instructions)
	}
}
