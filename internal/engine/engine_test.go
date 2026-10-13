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

func TestProjectGroupRouterStaysInsideBoundRepo(t *testing.T) {
	home := t.TempDir()
	cfg, library, globalActive := testConfig(t, home)
	writeSkill(t, library, "release", "Ship the app")
	if err := os.MkdirAll(globalActive, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(library, "release"), filepath.Join(globalActive, "release")); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(home, "app")
	other := filepath.Join(home, "other")
	cfg.SetGroup(core.Group{
		Name: "app", Description: "Skills for the app repository.",
		Projects: []string{project}, Members: []string{"release"},
	})
	cfg.SetMode("release", "codex", "global", core.ModeOff)
	cfg.SetMode("release", "codex", project, core.Mode("group:app"))

	globalCat, err := catalog.Scan(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	globalPlan, err := BuildPlan(cfg, globalCat, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(globalPlan.Conflicts) != 0 {
		t.Fatalf("global conflicts = %#v", globalPlan.Conflicts)
	}
	for _, op := range globalPlan.Operations {
		if strings.Contains(op.Path, "routers") || strings.HasSuffix(op.Path, "/app") {
			t.Fatalf("global plan exposed project router: %#v", op)
		}
	}

	projectCat, err := catalog.Scan(cfg, project)
	if err != nil {
		t.Fatal(err)
	}
	projectPlan, err := BuildPlan(cfg, projectCat, "codex", project)
	if err != nil {
		t.Fatal(err)
	}
	if len(projectPlan.Conflicts) != 0 {
		t.Fatalf("project conflicts = %#v", projectPlan.Conflicts)
	}
	if _, err := Apply(cfg, projectPlan); err != nil {
		t.Fatal(err)
	}
	router := filepath.Join(project, ".agents", "skills", "app", "SKILL.md")
	data, err := os.ReadFile(router)
	if err != nil || !strings.Contains(string(data), "release") {
		t.Fatalf("router = %s, err = %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(other, ".agents", "skills", "app")); !os.IsNotExist(err) {
		t.Fatalf("router leaked into unbound project: %v", err)
	}
}

func TestCursorProjectRouterIsSlashInvocable(t *testing.T) {
	home := t.TempDir()
	cfg, library, _ := testConfig(t, home)
	active := filepath.Join(home, "cursor-skills")
	cfg.Agents = map[string]core.Agent{"cursor": {Name: "cursor", SkillsDir: active}}
	writeSkill(t, library, "release", "Ship the app")
	project := filepath.Join(home, "app")
	cfg.SetGroup(core.Group{
		Name: "app", Description: "Skills for the app repository.",
		Projects: []string{project}, Members: []string{"release"},
	})
	cfg.SetMode("release", "cursor", "global", core.ModeOff)
	cfg.SetMode("release", "cursor", project, core.Mode("group:app"))
	cat, err := catalog.Scan(cfg, project)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(cfg, cat, "cursor", project)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Context) != 1 || plan.Context[0].AfterChars == 0 {
		t.Fatalf("context = %#v", plan.Context)
	}
	if _, err := Apply(cfg, plan); err != nil {
		t.Fatal(err)
	}
	router := filepath.Join(project, ".cursor", "skills", "app")
	info, err := os.Lstat(router)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("cursor project router should be a real directory, mode = %v", info.Mode())
	}
	data, err := os.ReadFile(filepath.Join(router, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "disable-model-invocation") {
		t.Fatalf("project router should stay model-invocable:\n%s", data)
	}
	group := core.Group{Name: "app", Projects: []string{project}}
	skills := []core.Skill{{Name: "release", Description: "Ship the app", Source: filepath.Join(library, "release")}}
	claude, _ := routerContent("claude", group, skills)
	if strings.Contains(claude, "disable-model-invocation") {
		t.Fatalf("claude project router = %s", claude)
	}
	_, policy := routerContent("codex", group, skills)
	if policy != "" {
		t.Fatalf("codex project router policy = %q", policy)
	}

	outside := filepath.Join(cfg.Settings.DataDir, "routers", "cursor", "app")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(router); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, router); err != nil {
		t.Fatal(err)
	}
	plan, err = BuildPlan(cfg, cat, "cursor", project)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(cfg, plan); err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(router)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("symlink router was not replaced with a directory: %v", info.Mode())
	}
	plan, err = BuildPlan(cfg, cat, "cursor", project)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("second plan = %#v", plan)
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
