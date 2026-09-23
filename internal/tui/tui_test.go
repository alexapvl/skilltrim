package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexapvl/skilltrim/internal/catalog"
	"github.com/alexapvl/skilltrim/internal/config"
	"github.com/alexapvl/skilltrim/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyboardFlowStagesPreviewsAndApplies(t *testing.T) {
	home := t.TempDir()
	library := filepath.Join(home, "library")
	active := filepath.Join(home, "active")
	configPath := filepath.Join(home, "config.toml")
	writeSkill(t, library, "alpha")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(library, "alpha"), filepath.Join(active, "alpha")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(home)
	cfg.Sources = []string{library}
	cfg.Agents = map[string]core.Agent{"codex": {Name: "codex", SkillsDir: active}}
	cat, err := catalog.Scan(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(cfg, configPath, "", "codex", cat)
	m.width, m.height = 100, 30
	if view := m.View(); !strings.Contains(view, "skilltrim") || !strings.Contains(view, "alpha") {
		t.Fatalf("unexpected view: %s", view)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if got := m.mode(cat.Skills[0]); got != core.ModeExplicit {
		t.Fatalf("mode after first space = %q", got)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if got := m.mode(cat.Skills[0]); got != core.ModeOff {
		t.Fatalf("mode after second space = %q", got)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if m.screen != screenPlan || len(m.plan.Operations) != 1 {
		t.Fatalf("plan screen = %d, plan = %#v", m.screen, m.plan)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if m.screen != screenConfirm {
		t.Fatalf("screen = %d", m.screen)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if m.screen != screenSkills || m.dirty {
		t.Fatalf("screen = %d, dirty = %v, status = %s", m.screen, m.dirty, m.status)
	}
	if _, err := os.Lstat(filepath.Join(active, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("alpha should be disabled: %v", err)
	}
	loaded, err := config.Load(configPath, home)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Mode("alpha", "codex", "global", true); got != core.ModeOff {
		t.Fatalf("saved mode = %q", got)
	}
}

func TestSearchFiltersSkills(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Agents = map[string]core.Agent{"codex": {Name: "codex", SkillsDir: t.TempDir()}}
	cat := core.Catalog{Skills: []core.Skill{{Name: "alpha"}, {Name: "beta"}}}
	m := newModel(cfg, filepath.Join(t.TempDir(), "config.toml"), "", "codex", cat)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if items := m.filteredSkills(); len(items) != 1 || items[0].Name != "beta" {
		t.Fatalf("filtered = %#v", items)
	}
}

func TestUnsupportedAgentSkipsExplicitAndDirtyQuitConfirms(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Agents = map[string]core.Agent{"cursor": {Name: "cursor", SkillsDir: t.TempDir()}}
	cat := core.Catalog{Skills: []core.Skill{{Name: "alpha", Source: filepath.Join(t.TempDir(), "alpha"), ActiveAgents: []string{"cursor"}, ImplicitAgents: []string{"cursor"}}}}
	m := newModel(cfg, filepath.Join(t.TempDir(), "config.toml"), "", "cursor", cat)
	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if got := m.mode(cat.Skills[0]); got != core.ModeOff {
		t.Fatalf("cursor mode = %q, want off", got)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.screen != screenDiscard {
		t.Fatalf("screen = %d, want discard confirmation", m.screen)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if m.screen != screenSkills {
		t.Fatalf("screen = %d, want skills", m.screen)
	}
}

func update(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	updated, _ := m.Update(msg)
	result, ok := updated.(model)
	if !ok {
		t.Fatalf("model type = %T", updated)
	}
	return result
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
