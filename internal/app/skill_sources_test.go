package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// newSkillSourceHome sets up a home with claude and codex on PATH (plus git),
// an empty tackroom.yaml and an upstream repo whose first commit is ten days
// old and whose second, which changes tdd, is from today.
func newSkillSourceHome(t *testing.T) (home string, configPath string, upstream string, oldCommit string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	dir := fakePath(t, "claude", "codex")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+filepath.Dir(gitPath))

	upstream = filepath.Join(home, "upstream")
	writeSkill := func(rel, body string) {
		writeSyncTestFile(t, filepath.Join(upstream, rel, "SKILL.md"), []byte("---\nname: "+filepath.Base(rel)+"\ndescription: d\n---\n"+body+"\n"))
	}
	writeSkill("skills/eng/tdd", "old tdd")
	writeSkill("skills/eng/grill", "grill")
	writeSkill("skills/misc/notes", "notes")
	writeSkill(".hidden/skills/secret", "never offered")
	git(t, upstream, "init", "-q", "-b", "main")
	git(t, upstream, "add", ".")
	commitAt(t, upstream, time.Now().Add(-10*24*time.Hour), "old")
	oldCommit = git(t, upstream, "rev-parse", "HEAD")
	writeSkill("skills/eng/tdd", "new tdd")
	git(t, upstream, "add", ".")
	commitAt(t, upstream, time.Now(), "new")

	repoRoot := filepath.Join(home, ".agents")
	configPath = filepath.Join(repoRoot, "tackroom.yaml")
	writeSyncTestFile(t, configPath, []byte(`version: 1
agents:
  - name: claude
    enabled: true
    skill_root: ~/.claude/skills
    detect: claude
  - name: codex
    enabled: true
    skill_root: ~/.codex/skills
    detect: codex
external_skills: [] # add third-party skill repos with `+"`tackroom skill sources add`"+`
`))
	return home, configPath, upstream, oldCommit
}

func commitAt(t *testing.T, dir string, when time.Time, message string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", message)
	cmd.Dir = dir
	stamp := when.Format(time.RFC3339)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, output)
	}
}

func TestSkillSourcesAddPinsAnOldCommitAndReachesEveryAgent(t *testing.T) {
	home, configPath, upstream, oldCommit := newSkillSourceHome(t)
	var out bytes.Buffer
	if err := runSkillSourcesAdd([]string{"file://" + upstream, "--skills", "tdd,notes", "--config", configPath}, nil, &out, false); err != nil {
		t.Fatalf("add: %v\n%s", err, out.String())
	}
	cfgData, _ := os.ReadFile(configPath)
	var cfg config
	if err := yaml.Unmarshal(cfgData, &cfg); err != nil || len(cfg.ExternalSkills) != 1 {
		t.Fatalf("tackroom.yaml: %v\n%s", err, cfgData)
	}
	src := cfg.ExternalSkills[0]
	if src.URL != "file://"+upstream || !src.Materialize || strings.Join(src.SkillDirs, ",") != "skills/eng,skills/misc" || strings.Join(src.Skills, ",") != "notes,tdd" {
		t.Fatalf("source entry = %#v", src)
	}
	if !strings.Contains(string(cfgData), "detect: codex") {
		t.Fatalf("the rest of tackroom.yaml was lost:\n%s", cfgData)
	}
	lock, err := readLockFile(filepath.Join(home, ".agents"))
	if err != nil || len(lock.ExternalSkills) != 1 || lock.ExternalSkills[0].Commit != oldCommit {
		t.Fatalf("lock should pin the 10-day-old commit %s: %#v %v", oldCommit, lock, err)
	}
	if data, _ := os.ReadFile(filepath.Join(home, ".agents", "skills", "tdd", "SKILL.md")); !strings.Contains(string(data), "old tdd") {
		t.Fatalf("materialized tdd should come from the pinned commit:\n%s", data)
	}
	for _, link := range []string{".claude/skills/tdd", ".codex/skills/notes"} {
		if info, err := os.Lstat(filepath.Join(home, link)); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s should be linked after add (err=%v)\n%s", link, err, out.String())
		}
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "skills", "grill")); !os.IsNotExist(err) {
		t.Fatalf("an unselected skill was added (err=%v)", err)
	}
	// Adding the same source again is refused.
	if err := runSkillSourcesAdd([]string{"file://" + upstream, "--skills", "tdd", "--config", configPath}, nil, &bytes.Buffer{}, false); err == nil || !strings.Contains(err.Error(), "already in tackroom.yaml") {
		t.Fatalf("a second add of the same source should fail, got %v", err)
	}
}

func TestSkillSourcesAddLatestPinsTheNewestCommit(t *testing.T) {
	home, configPath, upstream, _ := newSkillSourceHome(t)
	newest := git(t, upstream, "rev-parse", "HEAD")
	if err := runSkillSourcesAdd([]string{"file://" + upstream, "--all", "--latest", "--no-sync", "--config", configPath}, nil, &bytes.Buffer{}, false); err != nil {
		t.Fatal(err)
	}
	lock, _ := readLockFile(filepath.Join(home, ".agents"))
	if len(lock.ExternalSkills) != 1 || lock.ExternalSkills[0].Commit != newest {
		t.Fatalf("--latest should pin %s: %#v", newest, lock)
	}
	if got := strings.Join(lock.ExternalSkills[0].Materialized.Values(), ","); got != "grill,notes,tdd" {
		t.Fatalf("--all should own every visible skill, got %s", got)
	}
	cfg, _ := os.ReadFile(configPath)
	if strings.Contains(string(cfg), "\n    skills:") || strings.Contains(string(cfg), "secret") {
		t.Fatalf("--all keeps no allowlist and skips hidden directories:\n%s", cfg)
	}
}

func TestSkillSourcesAddAsksForSkills(t *testing.T) {
	_, configPath, upstream, _ := newSkillSourceHome(t)
	err := runSkillSourcesAdd([]string{"file://" + upstream, "--no-sync", "--config", configPath}, nil, &bytes.Buffer{}, false)
	if err == nil || !strings.Contains(err.Error(), "--skills a,b or --all") || !strings.Contains(err.Error(), "grill") {
		t.Fatalf("without a terminal, add should list the skills and ask for a flag, got %v", err)
	}
	var out bytes.Buffer
	if err := runSkillSourcesAdd([]string{"file://" + upstream, "--no-sync", "--config", configPath}, strings.NewReader("1, tdd\n"), &out, true); err != nil {
		t.Fatalf("interactive add: %v\n%s", err, out.String())
	}
	cfg, _ := os.ReadFile(configPath)
	if !strings.Contains(string(cfg), "- grill") || !strings.Contains(string(cfg), "- tdd") || strings.Contains(string(cfg), "- notes") {
		t.Fatalf("interactive pick should select grill and tdd:\n%s", cfg)
	}
}

func TestSkillSourcesAddAdoptsAnIdenticalCopyAndRefusesADifferentOne(t *testing.T) {
	home, configPath, upstream, oldCommit := newSkillSourceHome(t)
	target := filepath.Join(home, ".agents", "skills", "grill")
	writeSyncTestFile(t, filepath.Join(target, "SKILL.md"), []byte("mine\n"))
	err := runSkillSourcesAdd([]string{"file://" + upstream, "--skills", "grill", "--no-sync", "--config", configPath}, nil, &bytes.Buffer{}, false)
	if err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("a differing canonical skill should stop the add, got %v", err)
	}
	if cfg, _ := os.ReadFile(configPath); strings.Contains(string(cfg), "upstream") {
		t.Fatalf("a refused add must not touch tackroom.yaml:\n%s", cfg)
	}
	// An identical copy, as `npx skills add` leaves it, is adopted.
	identical := git(t, upstream, "show", oldCommit+":skills/eng/grill/SKILL.md")
	writeSyncTestFile(t, filepath.Join(target, "SKILL.md"), []byte(identical+"\n"))
	if err := runSkillSourcesAdd([]string{"file://" + upstream, "--skills", "grill", "--config", configPath}, nil, &bytes.Buffer{}, false); err != nil {
		t.Fatalf("an identical copy should be adopted: %v", err)
	}
}

func TestExcludeAgentsKeepsASourceOutOfThatAgent(t *testing.T) {
	home, configPath, upstream, _ := newSkillSourceHome(t)
	if err := runSkillSourcesAdd([]string{"file://" + upstream, "--skills", "tdd", "--config", configPath}, nil, &bytes.Buffer{}, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(configPath)
	writeSyncTestFile(t, configPath, []byte(strings.Replace(string(data), "materialize: true", "materialize: true\n    exclude_agents: [claude]", 1)))
	if err := runSync(runOptions{ConfigPath: configPath, Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "skills", "tdd")); !os.IsNotExist(err) {
		t.Fatalf("claude should lose the excluded source's skill (err=%v)", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codex", "skills", "tdd")); err != nil {
		t.Fatalf("codex keeps it: %v", err)
	}
	if err := runStatus(runOptions{ConfigPath: configPath, Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("status after sync: %v", err)
	}
}

func TestSameRepoNameNeedsADistinctSourceName(t *testing.T) {
	home := t.TempDir()
	cfg := config{Version: 1, ExternalSkills: []externalSkillSource{
		{URL: "https://github.com/mattpocock/skills"},
		{URL: "https://github.com/anthropics/skills"},
	}}
	if err := validateConfig(&cfg, home, false); err == nil || !strings.Contains(err.Error(), "name:") {
		t.Fatalf("two repos named skills should need a name, got %v", err)
	}
	cfg.ExternalSkills[1].Name = "anthropics-skills"
	if err := validateConfig(&cfg, home, false); err != nil {
		t.Fatal(err)
	}
	if sourceName(cfg.ExternalSkills[1]) != "anthropics-skills" || sourceName(cfg.ExternalSkills[0]) != "skills" {
		t.Fatalf("source names = %s, %s", sourceName(cfg.ExternalSkills[0]), sourceName(cfg.ExternalSkills[1]))
	}
	cfg.ExternalSkills[1].Name = "../x"
	if err := validateConfig(&cfg, home, false); err == nil {
		t.Fatal("a path-like source name must be rejected")
	}
}

func TestDetectInstalledSources(t *testing.T) {
	home := t.TempDir()
	plugins := filepath.Join(home, ".claude", "plugins")
	official := filepath.Join(plugins, "marketplaces", "official")
	writeSyncTestFile(t, filepath.Join(plugins, "known_marketplaces.json"), []byte(`{"official":{"source":{"source":"github","repo":"anthropics/claude-plugins-official"},"installLocation":"`+official+`"}}`))
	writeSyncTestFile(t, filepath.Join(official, ".claude-plugin", "marketplace.json"), []byte(`{"plugins":[
  {"name":"superpowers","source":{"source":"url","url":"https://github.com/obra/superpowers.git"}},
  {"name":"frontend-design","source":"./plugins/frontend-design"},
  {"name":"no-skills","source":{"source":"github","repo":"x/mcp-only"}}]}`))
	installed := map[string]string{"superpowers": "a", "frontend-design": "b", "no-skills": "c"}
	var entries []string
	for name, dir := range installed {
		installPath := filepath.Join(plugins, "cache", dir)
		if name != "no-skills" {
			writeSyncTestFile(t, filepath.Join(installPath, "skills", name, "SKILL.md"), []byte("x"))
		} else {
			writeSyncTestFile(t, filepath.Join(installPath, ".mcp.json"), []byte("{}"))
		}
		entries = append(entries, `"`+name+`@official":[{"installPath":"`+installPath+`"}]`)
	}
	writeSyncTestFile(t, filepath.Join(plugins, "installed_plugins.json"), []byte(`{"version":2,"plugins":{`+strings.Join(entries, ",")+`}}`))
	t.Setenv("XDG_STATE_HOME", "")
	writeSyncTestFile(t, filepath.Join(home, ".agents", ".skill-lock.json"), []byte(`{"version":3,"skills":{
  "tdd":{"source":"mattpocock/skills","sourceType":"github"},
  "grill-me":{"source":"mattpocock/skills","sourceType":"github"},
  "docs":{"source":"mintlify/bun.com","sourceType":"mintlify"}}}`))

	var got []string
	for _, found := range detectInstalledSources(home) {
		got = append(got, sourceSlugWithPath(found.Repo, found.Path)+" | "+found.Via)
	}
	want := []string{
		"anthropics/claude-plugins-official/plugins/frontend-design | Claude Code plugin frontend-design@official",
		"mattpocock/skills | npx skills (grill-me, tdd)",
		"obra/superpowers | Claude Code plugin superpowers@official",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("detected:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if plugin := matchingClaudePlugin(home, "obra/superpowers", ""); plugin != "superpowers@official" {
		t.Fatalf("superpowers should match its Claude plugin, got %q", plugin)
	}
	if plugin := matchingClaudePlugin(home, "anthropics/claude-plugins-official", "plugins/other"); plugin != "" {
		t.Fatalf("a different path in the same repo is not the same plugin, got %q", plugin)
	}
}

func TestSearchSkillsRegistryGroupsHitsBySource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" || r.URL.Query().Get("q") != "tdd" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"skills":[
  {"name":"tdd","source":"mattpocock/skills","installs":100},
  {"name":"tdd","source":"someone/else","installs":500},
  {"name":"grill-me","source":"mattpocock/skills","installs":900},
  {"name":"docs","source":"mintlify/bun.com/docs","installs":9999}]}`))
	}))
	defer server.Close()
	previous := skillsRegistryURL
	skillsRegistryURL = server.URL
	defer func() { skillsRegistryURL = previous }()

	sources, err := searchSkillsRegistry("tdd")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].Repo != "mattpocock/skills" || sources[0].Installs != 1000 || strings.Join(sources[0].Skills, ",") != "grill-me,tdd" {
		t.Fatalf("grouped sources = %#v", sources)
	}
}

func TestParseSourceArg(t *testing.T) {
	cases := []struct{ in, url, sub, branch string }{
		{"mattpocock/skills", "https://github.com/mattpocock/skills", "", ""},
		{"cursor/plugins/pstack", "https://github.com/cursor/plugins", "pstack", ""},
		{"https://github.com/cursor/plugins/tree/main/pstack", "https://github.com/cursor/plugins", "pstack", "main"},
		{"https://github.com/obra/superpowers.git", "https://github.com/obra/superpowers", "", ""},
		{"git@github.com:obra/superpowers.git", "https://github.com/obra/superpowers", "", ""},
		{"https://gitlab.com/a/b.git", "https://gitlab.com/a/b.git", "", ""},
	}
	for _, c := range cases {
		url, sub, branch, err := parseSourceArg(c.in)
		if err != nil || url != c.url || sub != c.sub || branch != c.branch {
			t.Errorf("parseSourceArg(%q) = %q %q %q %v", c.in, url, sub, branch, err)
		}
	}
	for _, bad := range []string{"skills", "a/b/../c"} {
		if _, _, _, err := parseSourceArg(bad); err == nil {
			t.Errorf("parseSourceArg(%q) should fail", bad)
		}
	}
}
