package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexapvl/skilltrim/internal/core"
)

func TestSaveLoadAndProjectTarget(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	cfg := Default(home)
	cfg.SetGroup(core.Group{Name: "apple", Members: []string{"build", "upload"}})
	cfg.SetMode("build", "codex", "global", core.Mode("group:apple"))
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "skills_dir") || strings.Contains(string(data), "SkillsDir") {
		t.Fatalf("unexpected TOML: %s", data)
	}
	loaded, err := Load(path, home)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Mode("build", "codex", "global", false); got != "group:apple" {
		t.Fatalf("mode = %q", got)
	}
	project := filepath.Join(home, "project")
	target, err := loaded.TargetDir("codex", project)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(project, ".agents", "skills")
	if target != want {
		t.Fatalf("target = %q, want %q", target, want)
	}
}

func TestValidateRejectsInvalidMode(t *testing.T) {
	cfg := Default(t.TempDir())
	cfg.Rules = []core.Rule{{Skill: "build", Agent: "codex", Scope: "global", Mode: "sometimes"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid mode error")
	}
}
