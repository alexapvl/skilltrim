package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexapvl/skilltrim/internal/config"
	"github.com/alexapvl/skilltrim/internal/core"
)

func TestScanDiscoversSourcesAndActiveLinks(t *testing.T) {
	home := t.TempDir()
	library := filepath.Join(home, "library")
	active := filepath.Join(home, "active")
	writeSkill(t, library, "alpha", "Alpha does useful work.")
	writeSkill(t, library, "beta", "Beta stays hidden.")
	policyDir := filepath.Join(library, "alpha", "agents")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyDir, "openai.yaml"), []byte("policy:\n  allow_implicit_invocation: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(library, "alpha"), filepath.Join(active, "alpha")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(library, "missing"), filepath.Join(active, "broken")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(home)
	cfg.Sources = []string{library}
	cfg.Agents = map[string]core.Agent{"codex": {Name: "codex", SkillsDir: active}}
	cat, err := Scan(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Skills) != 2 {
		t.Fatalf("skills = %d", len(cat.Skills))
	}
	alpha, ok := Find(cat, "alpha")
	if !ok || len(alpha.ActiveAgents) != 1 || alpha.ActiveAgents[0] != "codex" {
		t.Fatalf("unexpected alpha: %#v", alpha)
	}
	if alpha.DescriptionChars != 23 {
		t.Fatalf("description chars = %d", alpha.DescriptionChars)
	}
	if len(cat.Warnings) != 1 || cat.Warnings[0].Code != "broken_link" {
		t.Fatalf("warnings = %#v", cat.Warnings)
	}
	if cat.Agents[0].Active != 1 || cat.Agents[0].Explicit != 1 || cat.Agents[0].ContextChars != 0 {
		t.Fatalf("summary = %#v", cat.Agents[0])
	}
	if alpha.Targets[0].Mode != core.ModeExplicit {
		t.Fatalf("inferred mode = %q", alpha.Targets[0].Mode)
	}
}

func TestMatchGlob(t *testing.T) {
	cat := core.Catalog{Skills: []core.Skill{{Name: "asc-build"}, {Name: "asc-upload"}, {Name: "other"}}}
	matches, err := Match(cat, "asc-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d", len(matches))
	}
}

func TestScanIgnoresGeneratedRoutersAsDuplicateSources(t *testing.T) {
	home := t.TempDir()
	canonical := filepath.Join(home, "canonical")
	dataDir := filepath.Join(home, "data")
	writeSkill(t, canonical, "suite", "Canonical router.")

	agents := map[string]core.Agent{}
	for _, agent := range []string{"claude", "codex", "cursor"} {
		active := filepath.Join(home, agent)
		if err := os.MkdirAll(active, 0o755); err != nil {
			t.Fatal(err)
		}
		target := canonical
		if agent != "codex" {
			target = filepath.Join(dataDir, "routers", agent)
			writeSkill(t, target, "suite", "Generated router.")
		}
		if err := os.Symlink(filepath.Join(target, "suite"), filepath.Join(active, "suite")); err != nil {
			t.Fatal(err)
		}
		agents[agent] = core.Agent{Name: agent, SkillsDir: active}
	}

	cfg := config.Default(home)
	cfg.Sources = nil
	cfg.Settings.DataDir = dataDir
	cfg.Agents = agents
	cat, err := Scan(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range cat.Warnings {
		if warning.Code == "duplicate_skill" {
			t.Fatalf("generated routers reported as duplicates: %#v", cat.Warnings)
		}
	}
}

func writeSkill(t *testing.T, root, name, description string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: >\n  " + description + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
