package core

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

var Version = "0.1.0-dev"

type Mode string

const (
	ModeAuto     Mode = "auto"
	ModeExplicit Mode = "explicit"
	ModeOff      Mode = "off"
)

func ParseMode(value string) (Mode, error) {
	if value == string(ModeAuto) || value == string(ModeExplicit) || value == string(ModeOff) {
		return Mode(value), nil
	}
	if strings.HasPrefix(value, "group:") && ValidName(strings.TrimPrefix(value, "group:")) {
		return Mode(value), nil
	}
	return "", fmt.Errorf("invalid mode %q: use auto, explicit, group:<name>, or off", value)
}

func (m Mode) Group() string {
	return strings.TrimPrefix(string(m), "group:")
}

func (m Mode) IsGroup() bool {
	return strings.HasPrefix(string(m), "group:")
}

func ValidName(name string) bool {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return true
}

type Agent struct {
	Name      string `toml:"-" json:"name"`
	SkillsDir string `toml:"skills_dir" json:"skills_dir"`
}

type Rule struct {
	Skill string `toml:"skill" json:"skill"`
	Agent string `toml:"agent" json:"agent"`
	Scope string `toml:"scope,omitempty" json:"scope,omitempty"`
	Mode  Mode   `toml:"mode" json:"mode"`
}

type Group struct {
	Name        string   `toml:"name" json:"name"`
	Description string   `toml:"description,omitempty" json:"description,omitempty"`
	Members     []string `toml:"members,omitempty" json:"members"`
}

type TargetStatus struct {
	Agent string `json:"agent"`
	Mode  Mode   `json:"mode"`
	Scope string `json:"scope"`
}

type Skill struct {
	Name             string         `json:"name"`
	Description      string         `json:"description,omitempty"`
	Source           string         `json:"source"`
	DescriptionChars int            `json:"description_chars"`
	ContextChars     int            `json:"context_chars"`
	ActiveAgents     []string       `json:"active_agents"`
	ImplicitAgents   []string       `json:"implicit_agents,omitempty"`
	Targets          []TargetStatus `json:"targets,omitempty"`
	DuplicateSources []string       `json:"duplicate_sources,omitempty"`
}

type AgentSummary struct {
	Name            string `json:"name"`
	SkillsDir       string `json:"skills_dir"`
	Active          int    `json:"active"`
	Explicit        int    `json:"explicit"`
	ContextChars    int    `json:"context_chars"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type Catalog struct {
	Skills   []Skill        `json:"skills"`
	Agents   []AgentSummary `json:"agents"`
	Warnings []Warning      `json:"warnings,omitempty"`
}

func SortSkills(skills []Skill) {
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
}

func Scope(project string) string {
	if project == "" {
		return "global"
	}
	abs, err := filepath.Abs(project)
	if err != nil {
		return filepath.Clean(project)
	}
	return filepath.Clean(abs)
}
