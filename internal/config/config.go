package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alexapvl/skilltrim/internal/core"
	"github.com/pelletier/go-toml/v2"
)

type Settings struct {
	DataDir  string `toml:"data_dir,omitempty" json:"data_dir"`
	StateDir string `toml:"state_dir,omitempty" json:"state_dir"`
}

type File struct {
	Version  int                   `toml:"version" json:"version"`
	Sources  []string              `toml:"sources,omitempty" json:"sources"`
	Settings Settings              `toml:"settings" json:"settings"`
	Agents   map[string]core.Agent `toml:"agents" json:"agents"`
	Rules    []core.Rule           `toml:"rules,omitempty" json:"rules"`
	Groups   []core.Group          `toml:"groups,omitempty" json:"groups"`
}

func Default(home string) File {
	return File{
		Version: 1,
		Sources: []string{
			filepath.Join(home, ".local", "share", "agent-skills"),
			filepath.Join(home, ".agents", "routed-skills"),
		},
		Settings: Settings{
			DataDir:  filepath.Join(home, ".local", "share", "skilltrim"),
			StateDir: filepath.Join(home, ".local", "state", "skilltrim"),
		},
		Agents: map[string]core.Agent{
			"codex":    {Name: "codex", SkillsDir: filepath.Join(home, ".agents", "skills")},
			"claude":   {Name: "claude", SkillsDir: filepath.Join(home, ".claude", "skills")},
			"cursor":   {Name: "cursor", SkillsDir: filepath.Join(home, ".cursor", "skills")},
			"opencode": {Name: "opencode", SkillsDir: filepath.Join(home, ".config", "opencode", "skills")},
		},
	}
}

func DefaultPath(home string) string {
	if value := os.Getenv("SKILLTRIM_CONFIG"); value != "" {
		return Expand(value, home)
	}
	return filepath.Join(home, ".config", "skilltrim", "config.toml")
}

func Load(path, home string) (File, error) {
	cfg := Default(home)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return File{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Version != 1 {
		return File{}, fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if cfg.Agents == nil {
		cfg.Agents = Default(home).Agents
	}
	cfg.normalize(home)
	if err := cfg.Validate(); err != nil {
		return File{}, err
	}
	return cfg, nil
}

func (cfg *File) normalize(home string) {
	for i := range cfg.Sources {
		cfg.Sources[i] = Expand(cfg.Sources[i], home)
	}
	cfg.Settings.DataDir = Expand(cfg.Settings.DataDir, home)
	cfg.Settings.StateDir = Expand(cfg.Settings.StateDir, home)
	if cfg.Settings.DataDir == "" {
		cfg.Settings.DataDir = filepath.Join(home, ".local", "share", "skilltrim")
	}
	if cfg.Settings.StateDir == "" {
		cfg.Settings.StateDir = filepath.Join(home, ".local", "state", "skilltrim")
	}
	for name, agent := range cfg.Agents {
		agent.Name = name
		agent.SkillsDir = Expand(agent.SkillsDir, home)
		cfg.Agents[name] = agent
	}
	for i := range cfg.Rules {
		if cfg.Rules[i].Scope == "" {
			cfg.Rules[i].Scope = "global"
		}
	}
	for i := range cfg.Groups {
		sort.Strings(cfg.Groups[i].Members)
	}
}

func Save(path string, cfg File) error {
	data, err := Encode(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func Encode(cfg File) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return data, nil
}

func (cfg File) Validate() error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	for name, agent := range cfg.Agents {
		if !core.ValidName(name) || agent.SkillsDir == "" {
			return fmt.Errorf("invalid agent %q", name)
		}
	}
	groups := map[string]bool{}
	for _, group := range cfg.Groups {
		if !core.ValidName(group.Name) {
			return fmt.Errorf("invalid group name %q", group.Name)
		}
		if groups[group.Name] {
			return fmt.Errorf("duplicate group %q", group.Name)
		}
		groups[group.Name] = true
		for _, member := range group.Members {
			if !core.ValidName(member) {
				return fmt.Errorf("invalid skill name %q in group %q", member, group.Name)
			}
		}
	}
	for _, rule := range cfg.Rules {
		if !core.ValidName(rule.Skill) {
			return fmt.Errorf("invalid skill name %q in rule", rule.Skill)
		}
		if !core.ValidName(rule.Agent) {
			return fmt.Errorf("invalid agent name %q in rule", rule.Agent)
		}
		if _, err := core.ParseMode(string(rule.Mode)); err != nil {
			return err
		}
	}
	return nil
}

func Expand(path, home string) string {
	path = os.ExpandEnv(path)
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return filepath.Clean(path)
}

func (cfg File) AgentNames() []string {
	names := make([]string, 0, len(cfg.Agents))
	for name := range cfg.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (cfg File) TargetDir(agent, project string) (string, error) {
	configured, ok := cfg.Agents[agent]
	if !ok {
		return "", fmt.Errorf("unknown agent %q", agent)
	}
	if project == "" {
		return configured.SkillsDir, nil
	}
	dir := map[string]string{
		"codex": ".agents/skills", "claude": ".claude/skills", "cursor": ".cursor/skills", "opencode": ".opencode/skills",
	}[agent]
	if dir == "" {
		return "", fmt.Errorf("agent %q has no project scope adapter", agent)
	}
	abs, err := filepath.Abs(project)
	if err != nil {
		return "", fmt.Errorf("resolve project: %w", err)
	}
	return filepath.Join(abs, filepath.FromSlash(dir)), nil
}

func (cfg File) Mode(skill, agent, scope string, active bool) core.Mode {
	if mode, ok := cfg.RuleMode(skill, agent, scope); ok {
		return mode
	}
	if active {
		return core.ModeAuto
	}
	return core.ModeOff
}

func (cfg File) RuleMode(skill, agent, scope string) (core.Mode, bool) {
	for i := len(cfg.Rules) - 1; i >= 0; i-- {
		rule := cfg.Rules[i]
		if rule.Skill == skill && rule.Agent == agent && rule.Scope == scope {
			return rule.Mode, true
		}
	}
	return "", false
}

func (cfg *File) SetMode(skill, agent, scope string, mode core.Mode) {
	for i := range cfg.Rules {
		if cfg.Rules[i].Skill == skill && cfg.Rules[i].Agent == agent && cfg.Rules[i].Scope == scope {
			cfg.Rules[i].Mode = mode
			return
		}
	}
	cfg.Rules = append(cfg.Rules, core.Rule{Skill: skill, Agent: agent, Scope: scope, Mode: mode})
	sort.Slice(cfg.Rules, func(i, j int) bool {
		a, b := cfg.Rules[i], cfg.Rules[j]
		return a.Skill+a.Agent+a.Scope < b.Skill+b.Agent+b.Scope
	})
}

func (cfg *File) AddSource(path string) bool {
	path = filepath.Clean(path)
	for _, source := range cfg.Sources {
		if filepath.Clean(source) == path {
			return false
		}
	}
	cfg.Sources = append(cfg.Sources, path)
	sort.Strings(cfg.Sources)
	return true
}

func (cfg File) Group(name string) (core.Group, bool) {
	for _, group := range cfg.Groups {
		if group.Name == name {
			return group, true
		}
	}
	return core.Group{}, false
}

func (cfg *File) SetGroup(group core.Group) {
	for i := range cfg.Groups {
		if cfg.Groups[i].Name == group.Name {
			cfg.Groups[i] = group
			return
		}
	}
	cfg.Groups = append(cfg.Groups, group)
	sort.Slice(cfg.Groups, func(i, j int) bool { return cfg.Groups[i].Name < cfg.Groups[j].Name })
}

func (cfg *File) DeleteGroup(name string) {
	for i := range cfg.Groups {
		if cfg.Groups[i].Name == name {
			cfg.Groups = append(cfg.Groups[:i], cfg.Groups[i+1:]...)
			return
		}
	}
}
