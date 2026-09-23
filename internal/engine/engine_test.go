package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexapvl/skilltrim/internal/catalog"
	"github.com/alexapvl/skilltrim/internal/config"
	"github.com/alexapvl/skilltrim/internal/core"
)

func TestApplyAndRollbackLinks(t *testing.T) {
	home := t.TempDir()
	cfg, library, active := testConfig(t, home)
	writeSkill(t, library, "alpha", "Alpha skill")
	writeSkill(t, library, "beta", "Beta skill")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(library, "alpha"), filepath.Join(active, "alpha")); err != nil {
		t.Fatal(err)
	}
	cfg.SetMode("alpha", "codex", "global", core.ModeOff)
	cfg.SetMode("beta", "codex", "global", core.ModeAuto)
	cat, err := catalog.Scan(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(cfg, cat, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 || len(plan.Conflicts) != 0 {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.Context) != 1 {
		t.Fatalf("context = %#v", plan.Context)
	}
	result, err := Apply(cfg, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 2 {
		t.Fatalf("applied = %d", result.Applied)
	}
	if _, err := os.Lstat(filepath.Join(active, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("alpha should be absent: %v", err)
	}
	wantBeta, err := filepath.EvalSymlinks(filepath.Join(library, "beta"))
	if err != nil {
		t.Fatal(err)
	}
	if target, err := filepath.EvalSymlinks(filepath.Join(active, "beta")); err != nil || target != wantBeta {
		t.Fatalf("beta target = %q, want = %q, err = %v", target, wantBeta, err)
	}
	rollback, err := Rollback(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rollback.Restored != 2 {
		t.Fatalf("restored = %d", rollback.Restored)
	}
	if _, err := filepath.EvalSymlinks(filepath.Join(active, "alpha")); err != nil {
		t.Fatalf("alpha not restored: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(active, "beta")); !os.IsNotExist(err) {
		t.Fatalf("beta should be absent: %v", err)
	}
}

func TestGeneratedRouterHidesMembers(t *testing.T) {
	home := t.TempDir()
	cfg, library, active := testConfig(t, home)
	writeSkill(t, library, "build", "Build releases")
	writeSkill(t, library, "upload", "Upload releases")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"build", "upload"} {
		if err := os.Symlink(filepath.Join(library, name), filepath.Join(active, name)); err != nil {
			t.Fatal(err)
		}
	}
	cfg.SetGroup(core.Group{Name: "asc", Members: []string{"build", "upload"}})
	cfg.SetMode("build", "codex", "global", core.Mode("group:asc"))
	cfg.SetMode("upload", "codex", "global", core.Mode("group:asc"))
	cat, err := catalog.Scan(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(cfg, cat, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Context) != 1 || plan.Context[0].SavedChars <= 0 || plan.Context[0].AfterChars != 0 {
		t.Fatalf("context = %#v", plan.Context)
	}
	result, err := Apply(cfg, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 4 {
		t.Fatalf("applied = %d, operations = %#v", result.Applied, plan.Operations)
	}
	for _, name := range []string{"build", "upload"} {
		if _, err := os.Lstat(filepath.Join(active, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should be hidden", name)
		}
	}
	router := filepath.Join(active, "asc")
	if _, err := filepath.EvalSymlinks(router); err != nil {
		t.Fatalf("router missing: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(cfg.Settings.DataDir, "routers", "codex", "asc", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "build") || !strings.Contains(string(content), "upload") {
		t.Fatalf("router content = %s", content)
	}
	policy, err := os.ReadFile(filepath.Join(cfg.Settings.DataDir, "routers", "codex", "asc", "agents", "openai.yaml"))
	if err != nil || !strings.Contains(string(policy), "allow_implicit_invocation: false") {
		t.Fatalf("router policy = %s, err = %v", policy, err)
	}
	cat, err = catalog.Scan(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, err := BuildPlan(cfg, cat, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPlan.Operations) != 0 || len(secondPlan.Conflicts) != 0 {
		t.Fatalf("second plan should be empty: %#v", secondPlan)
	}
}

func TestRefusesRealDirectory(t *testing.T) {
	home := t.TempDir()
	cfg, library, active := testConfig(t, home)
	writeSkill(t, library, "alpha", "Alpha skill")
	writeSkill(t, active, "alpha", "Local alpha")
	cfg.SetMode("alpha", "codex", "global", core.ModeOff)
	cat, err := catalog.Scan(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(cfg, cat, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 || len(plan.Operations) != 0 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestRefusesUnownedGeneratedDirectory(t *testing.T) {
	home := t.TempDir()
	cfg, library, _ := testConfig(t, home)
	writeSkill(t, library, "alpha", "Alpha skill")
	cfg.SetGroup(core.Group{Name: "pack", Members: []string{"alpha"}})
	cfg.SetMode("alpha", "codex", "global", core.Mode("group:pack"))
	unowned := filepath.Join(cfg.Settings.DataDir, "routers", "codex", "pack")
	if err := os.MkdirAll(unowned, 0o755); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Scan(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(cfg, cat, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0].Message, "ownership marker") {
		t.Fatalf("plan = %#v", plan)
	}
}

func testConfig(t *testing.T, home string) (config.File, string, string) {
	t.Helper()
	library := filepath.Join(home, "library")
	active := filepath.Join(home, "active")
	cfg := config.Default(home)
	cfg.Sources = []string{library}
	cfg.Agents = map[string]core.Agent{"codex": {Name: "codex", SkillsDir: active}}
	return cfg, library, active
}

func writeSkill(t *testing.T, root, name, description string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
