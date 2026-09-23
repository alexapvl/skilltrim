package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/alexapvl/skilltrim/internal/config"
	"github.com/alexapvl/skilltrim/internal/core"
	"gopkg.in/yaml.v3"
)

type frontmatter struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
}

type openAISettings struct {
	Policy struct {
		AllowImplicitInvocation *bool `yaml:"allow_implicit_invocation"`
	} `yaml:"policy"`
}

type occurrence struct {
	name        string
	description string
	path        string
	canonical   string
	activeAgent string
	implicit    bool
	isSource    bool
}

func Scan(cfg config.File, project string) (core.Catalog, error) {
	var warnings []core.Warning
	var occurrences []occurrence

	for _, root := range cfg.Sources {
		found, foundWarnings, err := scanRoot(root, "", true)
		if err != nil {
			return core.Catalog{}, err
		}
		occurrences = append(occurrences, found...)
		warnings = append(warnings, foundWarnings...)
	}

	summaries := make([]core.AgentSummary, 0, len(cfg.Agents))
	for _, agentName := range cfg.AgentNames() {
		root, err := cfg.TargetDir(agentName, project)
		if err != nil {
			return core.Catalog{}, err
		}
		found, foundWarnings, err := scanRoot(root, agentName, false)
		if err != nil {
			return core.Catalog{}, err
		}
		occurrences = append(occurrences, found...)
		warnings = append(warnings, foundWarnings...)
		summary := core.AgentSummary{Name: agentName, SkillsDir: root}
		for _, item := range found {
			summary.Active++
			if item.implicit {
				summary.ContextChars += contextChars(item.name, item.description, item.path)
			} else {
				summary.Explicit++
			}
		}
		summary.EstimatedTokens = (summary.ContextChars + 3) / 4
		summaries = append(summaries, summary)
	}

	byName := map[string][]occurrence{}
	for _, item := range occurrences {
		byName[item.name] = append(byName[item.name], item)
	}

	scope := core.Scope(project)
	skills := make([]core.Skill, 0, len(byName))
	for name, items := range byName {
		selected := chooseSource(items)
		activeSet := map[string]bool{}
		implicitSet := map[string]bool{}
		sourceSet := map[string]bool{}
		hasLibrarySource := false
		for _, item := range items {
			if item.isSource {
				hasLibrarySource = true
				break
			}
		}
		for _, item := range items {
			if item.activeAgent != "" {
				activeSet[item.activeAgent] = true
				if item.implicit {
					implicitSet[item.activeAgent] = true
				}
			}
			if !hasLibrarySource || item.isSource {
				sourceSet[item.canonical] = true
			}
		}
		activeAgents := sortedKeys(activeSet)
		implicitAgents := sortedKeys(implicitSet)
		duplicateSources := sortedKeys(sourceSet)
		if len(duplicateSources) == 1 {
			duplicateSources = nil
		} else if len(duplicateSources) > 1 {
			warnings = append(warnings, core.Warning{
				Code:    "duplicate_skill",
				Message: fmt.Sprintf("skill %q resolves to %d different sources", name, len(duplicateSources)),
			})
		}
		targets := make([]core.TargetStatus, 0, len(cfg.Agents))
		for _, agentName := range cfg.AgentNames() {
			mode, configured := cfg.RuleMode(name, agentName, scope)
			if !configured {
				switch {
				case implicitSet[agentName]:
					mode = core.ModeAuto
				case activeSet[agentName]:
					mode = core.ModeExplicit
				default:
					mode = core.ModeOff
				}
			}
			targets = append(targets, core.TargetStatus{
				Agent: agentName,
				Mode:  mode,
				Scope: scope,
			})
		}
		skills = append(skills, core.Skill{
			Name:             name,
			Description:      selected.description,
			Source:           selected.canonical,
			DescriptionChars: utf8.RuneCountInString(selected.description),
			ContextChars:     contextChars(name, selected.description, selected.canonical),
			ActiveAgents:     activeAgents,
			ImplicitAgents:   implicitAgents,
			Targets:          targets,
			DuplicateSources: duplicateSources,
		})
	}

	core.SortSkills(skills)
	sort.Slice(warnings, func(i, j int) bool {
		return warnings[i].Code+warnings[i].Path < warnings[j].Code+warnings[j].Path
	})
	return core.Catalog{Skills: skills, Agents: summaries, Warnings: warnings}, nil
}

func Find(catalog core.Catalog, name string) (core.Skill, bool) {
	i := sort.Search(len(catalog.Skills), func(i int) bool { return catalog.Skills[i].Name >= name })
	if i < len(catalog.Skills) && catalog.Skills[i].Name == name {
		return catalog.Skills[i], true
	}
	return core.Skill{}, false
}

func Match(catalog core.Catalog, pattern string) ([]core.Skill, error) {
	if !strings.ContainsAny(pattern, "*?[") {
		skill, ok := Find(catalog, pattern)
		if !ok {
			return nil, fmt.Errorf("skill %q not found", pattern)
		}
		return []core.Skill{skill}, nil
	}
	var matches []core.Skill
	for _, skill := range catalog.Skills {
		matched, err := filepath.Match(pattern, skill.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid skill pattern %q: %w", pattern, err)
		}
		if matched {
			matches = append(matches, skill)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no skills match %q", pattern)
	}
	return matches, nil
}

func WarningsForAgent(warnings []core.Warning, cfg config.File, selected, project string) []core.Warning {
	if selected == "" || selected == "all" {
		return warnings
	}
	target, err := cfg.TargetDir(selected, project)
	if err != nil {
		return warnings
	}
	result := make([]core.Warning, 0, len(warnings))
	for _, warning := range warnings {
		if warning.Path == "" || within(warning.Path, target) || !withinAnyAgent(warning.Path, cfg, project) {
			result = append(result, warning)
		}
	}
	return result
}

func scanRoot(root, activeAgent string, isSource bool) ([]occurrence, []core.Warning, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("scan %s: %w", root, err)
	}
	var found []occurrence
	var warnings []core.Warning
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			if entry.Type()&os.ModeSymlink != 0 {
				warnings = append(warnings, core.Warning{Code: "broken_link", Message: "broken skill symlink", Path: path})
			}
			continue
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.IsDir() {
			continue
		}
		skillFile := filepath.Join(canonical, "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			warnings = append(warnings, core.Warning{Code: "unreadable_skill", Message: err.Error(), Path: skillFile})
			continue
		}
		meta, err := parseFrontmatter(data)
		if err != nil {
			warnings = append(warnings, core.Warning{Code: "invalid_frontmatter", Message: err.Error(), Path: skillFile})
			continue
		}
		name := meta.Name
		if name == "" {
			name = entry.Name()
		}
		if !core.ValidName(name) {
			warnings = append(warnings, core.Warning{Code: "invalid_name", Message: fmt.Sprintf("invalid skill name %q", name), Path: skillFile})
			continue
		}
		found = append(found, occurrence{
			name: name, description: strings.TrimSpace(meta.Description), path: path,
			canonical: filepath.Clean(canonical), activeAgent: activeAgent,
			implicit: activeAgent == "" || isImplicit(activeAgent, meta, canonical), isSource: isSource,
		})
	}
	return found, warnings, nil
}

func isImplicit(agent string, meta frontmatter, canonical string) bool {
	if agent == "claude" && meta.DisableModelInvocation {
		return false
	}
	if agent != "codex" {
		return true
	}
	data, err := os.ReadFile(filepath.Join(canonical, "agents", "openai.yaml"))
	if err != nil {
		return true
	}
	var settings openAISettings
	if yaml.Unmarshal(data, &settings) != nil || settings.Policy.AllowImplicitInvocation == nil {
		return true
	}
	return *settings.Policy.AllowImplicitInvocation
}

func parseFrontmatter(data []byte) (frontmatter, error) {
	if !bytes.HasPrefix(data, []byte("---\n")) && !bytes.HasPrefix(data, []byte("---\r\n")) {
		return frontmatter{}, errors.New("SKILL.md has no YAML frontmatter")
	}
	lines := bytes.Split(data, []byte("\n"))
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return frontmatter{}, errors.New("SKILL.md frontmatter is not closed")
	}
	var meta frontmatter
	if err := yaml.Unmarshal(bytes.Join(lines[1:end], []byte("\n")), &meta); err != nil {
		return frontmatter{}, fmt.Errorf("parse YAML frontmatter: %w", err)
	}
	return meta, nil
}

func chooseSource(items []occurrence) occurrence {
	selected := items[0]
	for _, item := range items {
		if item.isSource && !selected.isSource {
			selected = item
		}
		if item.isSource == selected.isSource && item.canonical < selected.canonical {
			selected = item
		}
	}
	return selected
}

func contextChars(name, description, path string) int {
	return utf8.RuneCountInString(name) + utf8.RuneCountInString(description) + utf8.RuneCountInString(path)
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func withinAnyAgent(path string, cfg config.File, project string) bool {
	for _, agent := range cfg.AgentNames() {
		root, err := cfg.TargetDir(agent, project)
		if err == nil && within(path, root) {
			return true
		}
	}
	return false
}
