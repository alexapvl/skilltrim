package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIJSONPlanApplyRollback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SKILLTRIM_HOME", home)
	library := filepath.Join(home, ".local", "share", "agent-skills")
	active := filepath.Join(home, ".agents", "skills")
	writeSkill(t, library, "alpha")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(library, "alpha"), filepath.Join(active, "alpha")); err != nil {
		t.Fatal(err)
	}

	code, stdout := run(t)
	if code != 0 {
		t.Fatalf("dashboard code = %d: %s", code, stdout)
	}
	var dashboard map[string]any
	if err := json.Unmarshal([]byte(stdout), &dashboard); err != nil {
		t.Fatalf("dashboard is not JSON: %v\n%s", err, stdout)
	}
	if dashboard["skills"].(float64) != 1 {
		t.Fatalf("dashboard = %#v", dashboard)
	}

	code, stdout = run(t, "mode", "set", "alpha", "off", "--agent", "codex")
	if code != 0 || !strings.Contains(stdout, `"changed":1`) {
		t.Fatalf("mode code = %d: %s", code, stdout)
	}
	code, stdout = run(t, "plan", "--agent", "codex")
	if code != 0 || !strings.Contains(stdout, `"action":"remove_link"`) {
		t.Fatalf("plan code = %d: %s", code, stdout)
	}
	code, stdout = run(t, "apply", "--agent", "codex")
	if code != 0 || !strings.Contains(stdout, `"applied":1`) {
		t.Fatalf("apply code = %d: %s", code, stdout)
	}
	if _, err := os.Lstat(filepath.Join(active, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("alpha should be disabled: %v", err)
	}
	code, stdout = run(t, "rollback")
	if code != 0 || !strings.Contains(stdout, `"restored":1`) {
		t.Fatalf("rollback code = %d: %s", code, stdout)
	}
	if _, err := filepath.EvalSymlinks(filepath.Join(active, "alpha")); err != nil {
		t.Fatalf("alpha should be restored: %v", err)
	}
}

func TestCLITOONAndStructuredUsageError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SKILLTRIM_HOME", home)
	code, stdout := run(t, "list", "--toon")
	if code != 0 || !strings.Contains(stdout, "count: 0") || !strings.Contains(stdout, "skills[0]") {
		t.Fatalf("TOON code = %d: %s", code, stdout)
	}
	code, stdout = run(t, "mode", "set", "alpha", "off")
	if code != 2 {
		t.Fatalf("usage code = %d: %s", code, stdout)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(stdout), &response); err != nil || response["error"] == nil {
		t.Fatalf("error is not structured JSON: %v, %s", err, stdout)
	}
}

func TestCLIGroupAndExplicitFlows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SKILLTRIM_HOME", home)
	library := filepath.Join(home, ".local", "share", "agent-skills")
	active := filepath.Join(home, ".agents", "skills")
	for _, name := range []string{"asc-build", "asc-upload", "solo"} {
		writeSkill(t, library, name)
	}
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"asc-build", "asc-upload", "solo"} {
		if err := os.Symlink(filepath.Join(library, name), filepath.Join(active, name)); err != nil {
			t.Fatal(err)
		}
	}
	if code, stdout := run(t, "group", "create", "asc"); code != 0 {
		t.Fatalf("group create code = %d: %s", code, stdout)
	}
	if code, stdout := run(t, "group", "add", "asc", "asc-*", "--agent", "codex"); code != 0 || !strings.Contains(stdout, `"changed":2`) {
		t.Fatalf("group add code = %d: %s", code, stdout)
	}
	if code, stdout := run(t, "mode", "set", "solo", "explicit", "--agent", "codex"); code != 0 {
		t.Fatalf("explicit code = %d: %s", code, stdout)
	}
	if code, stdout := run(t, "apply", "--agent", "codex"); code != 0 || !strings.Contains(stdout, `"applied":6`) {
		t.Fatalf("apply code = %d: %s", code, stdout)
	}
	for _, name := range []string{"asc-build", "asc-upload"} {
		if _, err := os.Lstat(filepath.Join(active, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should be grouped: %v", name, err)
		}
	}
	if _, err := filepath.EvalSymlinks(filepath.Join(active, "asc")); err != nil {
		t.Fatalf("group router missing: %v", err)
	}
	policy := filepath.Join(home, ".local", "share", "skilltrim", "proxies", "codex", "solo", "agents", "openai.yaml")
	if data, err := os.ReadFile(policy); err != nil || !strings.Contains(string(data), "allow_implicit_invocation: false") {
		t.Fatalf("explicit policy = %s, err = %v", data, err)
	}
	if code, stdout := run(t, "plan", "--agent", "codex"); code != 0 || !strings.Contains(stdout, `"operations":[]`) {
		t.Fatalf("second plan code = %d: %s", code, stdout)
	}
	if code, stdout := run(t, "doctor", "--agent", "codex"); code != 0 || !strings.Contains(stdout, `"status":"ok"`) {
		t.Fatalf("doctor code = %d: %s", code, stdout)
	}
}

func TestCLIProjectScopeDoesNotTouchGlobalLink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SKILLTRIM_HOME", home)
	library := filepath.Join(home, ".local", "share", "agent-skills")
	globalActive := filepath.Join(home, ".agents", "skills")
	project := filepath.Join(home, "project")
	projectActive := filepath.Join(project, ".agents", "skills")
	writeSkill(t, library, "alpha")
	for _, active := range []string{globalActive, projectActive} {
		if err := os.MkdirAll(active, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(library, "alpha"), filepath.Join(active, "alpha")); err != nil {
			t.Fatal(err)
		}
	}
	if code, stdout := run(t, "mode", "set", "alpha", "off", "--agent", "codex", "--project", project); code != 0 {
		t.Fatalf("mode code = %d: %s", code, stdout)
	}
	if code, stdout := run(t, "apply", "--agent", "codex", "--project", project); code != 0 {
		t.Fatalf("apply code = %d: %s", code, stdout)
	}
	if _, err := os.Lstat(filepath.Join(projectActive, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("project alpha should be off: %v", err)
	}
	if _, err := filepath.EvalSymlinks(filepath.Join(globalActive, "alpha")); err != nil {
		t.Fatalf("global alpha changed: %v", err)
	}
}

func TestCLIMoveAcrossProjectsAndRollback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SKILLTRIM_HOME", home)
	library := filepath.Join(home, ".local", "share", "agent-skills")
	globalActive := filepath.Join(home, ".agents", "skills")
	writeSkill(t, library, "alpha")
	if err := os.MkdirAll(globalActive, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(library, "alpha"), filepath.Join(globalActive, "alpha")); err != nil {
		t.Fatal(err)
	}

	projects := []string{filepath.Join(home, "one"), filepath.Join(home, "two")}
	for _, project := range projects {
		excludeDir := filepath.Join(project, ".git", "info")
		if err := os.MkdirAll(excludeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(excludeDir, "exclude"), []byte("existing\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	args := []string{"move", "alpha", "--to-project", projects[0], "--to-project", projects[1], "--agent", "all"}
	if code, stdout := run(t, args...); code != 0 || !strings.Contains(stdout, `"status":"preview"`) {
		t.Fatalf("preview code = %d: %s", code, stdout)
	}
	if _, err := filepath.EvalSymlinks(filepath.Join(globalActive, "alpha")); err != nil {
		t.Fatalf("preview changed global link: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "skilltrim", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("preview wrote config: %v", err)
	}

	if code, stdout := run(t, append(args, "--apply")...); code != 0 || !strings.Contains(stdout, `"status":"applied"`) || !strings.Contains(stdout, `"applied":6`) {
		t.Fatalf("apply code = %d: %s", code, stdout)
	}
	if _, err := os.Lstat(filepath.Join(globalActive, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("global alpha should be absent: %v", err)
	}
	for _, project := range projects {
		local := filepath.Join(project, ".agents", "skills", "alpha")
		if _, err := filepath.EvalSymlinks(local); err != nil {
			t.Fatalf("project alpha missing: %v", err)
		}
		exclude, err := os.ReadFile(filepath.Join(project, ".git", "info", "exclude"))
		if err != nil || !strings.Contains(string(exclude), "/.agents/skills/alpha") {
			t.Fatalf("Git exclude = %q, err = %v", exclude, err)
		}
	}

	if code, stdout := run(t, "rollback"); code != 0 || !strings.Contains(stdout, `"restored":6`) {
		t.Fatalf("rollback code = %d: %s", code, stdout)
	}
	if _, err := filepath.EvalSymlinks(filepath.Join(globalActive, "alpha")); err != nil {
		t.Fatalf("global alpha not restored: %v", err)
	}
	for _, project := range projects {
		if _, err := os.Lstat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
			t.Fatalf("project alpha should be absent: %v", err)
		}
		if _, err := os.Stat(filepath.Join(project, ".agents")); !os.IsNotExist(err) {
			t.Fatalf("project activation directory should be removed: %v", err)
		}
		exclude, err := os.ReadFile(filepath.Join(project, ".git", "info", "exclude"))
		if err != nil || string(exclude) != "existing\n" {
			t.Fatalf("Git exclude not restored: %q, err = %v", exclude, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "skilltrim", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("config should be restored to absent: %v", err)
	}
}

func TestVersionIsBare(t *testing.T) {
	code, stdout := run(t, "--version")
	if code != 0 || stdout != "0.1.0\n" {
		t.Fatalf("version code = %d: %q", code, stdout)
	}
}

func TestUnknownFlagFailsLoud(t *testing.T) {
	code, stdout := run(t, "list", "--stat")
	if code != 2 || !strings.Contains(stdout, `"error":"unknown flag \"--stat\""`) {
		t.Fatalf("unknown flag code = %d: %s", code, stdout)
	}
}

func run(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return code, stdout.String()
}

func writeSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: Test skill\n---\n\n# Test\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
