package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alexapvl/skilltrim/internal/catalog"
	"github.com/alexapvl/skilltrim/internal/config"
	"github.com/alexapvl/skilltrim/internal/core"
)

type Operation struct {
	Action  string `json:"action"`
	Agent   string `json:"agent"`
	Path    string `json:"path"`
	Target  string `json:"target,omitempty"`
	Reason  string `json:"reason"`
	content string
	policy  string
}

type Conflict struct {
	Agent   string `json:"agent"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type Plan struct {
	Scope      string          `json:"scope"`
	Context    []ContextChange `json:"context"`
	Operations []Operation     `json:"operations"`
	Conflicts  []Conflict      `json:"conflicts"`
}

type ContextChange struct {
	Agent                string `json:"agent"`
	BeforeChars          int    `json:"before_chars"`
	AfterChars           int    `json:"after_chars"`
	SavedChars           int    `json:"saved_chars"`
	EstimatedTokensSaved int    `json:"estimated_tokens_saved"`
}

type ApplyResult struct {
	Applied  int    `json:"applied"`
	Snapshot string `json:"snapshot,omitempty"`
	Noop     bool   `json:"noop"`
}

type RollbackResult struct {
	Restored int  `json:"restored"`
	Noop     bool `json:"noop"`
}

type snapshot struct {
	CreatedAt time.Time       `json:"created_at"`
	Entries   []snapshotEntry `json:"entries"`
}

type snapshotEntry struct {
	Path         string `json:"path"`
	BeforeKind   string `json:"before_kind"`
	BeforeTarget string `json:"before_target,omitempty"`
	AfterTarget  string `json:"after_target,omitempty"`
}

const generatedMarker = ".skilltrim-generated"

func BuildPlan(cfg config.File, cat core.Catalog, selectedAgent, project string) (Plan, error) {
	agents, err := selectedAgents(cfg, selectedAgent)
	if err != nil {
		return Plan{}, err
	}
	scope := core.Scope(project)
	plan := Plan{Scope: scope, Context: []ContextChange{}, Operations: []Operation{}, Conflicts: []Conflict{}}
	desiredGenerated := map[string]bool{}

	for _, agent := range agents {
		targetDir, err := cfg.TargetDir(agent, project)
		if err != nil {
			return Plan{}, err
		}
		groups := map[string][]core.Skill{}
		modes := map[string]core.Mode{}
		configured := map[string]bool{}
		for _, skill := range cat.Skills {
			active := contains(skill.ActiveAgents, agent)
			mode, hasRule := cfg.RuleMode(skill.Name, agent, scope)
			if !hasRule {
				if contains(skill.ImplicitAgents, agent) {
					mode = core.ModeAuto
				} else if active {
					mode = core.ModeExplicit
				} else {
					mode = core.ModeOff
				}
			}
			modes[skill.Name] = mode
			configured[skill.Name] = hasRule
			if mode.IsGroup() {
				if _, ok := cfg.Group(mode.Group()); !ok {
					plan.Conflicts = append(plan.Conflicts, Conflict{Agent: agent, Path: filepath.Join(targetDir, skill.Name), Message: fmt.Sprintf("group %q is not configured", mode.Group())})
					continue
				}
				groups[mode.Group()] = append(groups[mode.Group()], skill)
			}
		}
		before := agentContext(cat, agent)
		after := 0
		for _, skill := range cat.Skills {
			mode := modes[skill.Name]
			active := contains(skill.ActiveAgents, agent)
			destination := filepath.Join(targetDir, skill.Name)
			if _, routerUsesName := groups[skill.Name]; routerUsesName {
				continue
			}
			if !configured[skill.Name] {
				if active && contains(skill.ImplicitAgents, agent) {
					after += contextChars(skill.Name, skill.Description, destination)
				}
				continue
			}
			switch {
			case mode == core.ModeAuto:
				after += contextChars(skill.Name, skill.Description, destination)
				planLink(&plan, agent, destination, skill.Source, "expose skill automatically")
			case mode == core.ModeExplicit:
				if agent != "codex" && agent != "claude" {
					plan.Conflicts = append(plan.Conflicts, Conflict{Agent: agent, Path: destination, Message: "explicit mode is unsupported by this agent adapter"})
					continue
				}
				proxy := filepath.Join(cfg.Settings.DataDir, "proxies", agent, skill.Name)
				desiredGenerated[proxy] = true
				content, policy := proxyContent(agent, skill)
				planGenerated(&plan, agent, proxy, "write_proxy", content, policy, "prepare explicit-only proxy")
				planLink(&plan, agent, destination, proxy, "expose skill only by explicit invocation")
			case mode.IsGroup():
				if _, ok := cfg.Group(mode.Group()); !ok {
					continue
				}
				planAbsent(&plan, agent, destination, "hide skill behind group router")
			case mode == core.ModeOff:
				planAbsent(&plan, agent, destination, "disable skill")
			}
		}

		groupNames := make([]string, 0, len(groups))
		for name := range groups {
			groupNames = append(groupNames, name)
		}
		sort.Strings(groupNames)
		for _, groupName := range groupNames {
			group, _ := cfg.Group(groupName)
			router := filepath.Join(cfg.Settings.DataDir, "routers", agent, groupName)
			desiredGenerated[router] = true
			content, policy := routerContent(agent, group, groups[groupName])
			planGenerated(&plan, agent, router, "write_router", content, policy, "prepare group router")
			routerDestination := filepath.Join(targetDir, groupName)
			planLink(&plan, agent, routerDestination, router, "expose group router")
			if agent != "codex" && agent != "claude" {
				description := group.Description
				if description == "" {
					description = fmt.Sprintf("Explicit router for %s skills.", group.Name)
				}
				after += contextChars(group.Name, description, routerDestination)
			}
		}

		removeStaleGenerated(&plan, agent, targetDir, cfg.Settings.DataDir, desiredGenerated)
		saved := before - after
		plan.Context = append(plan.Context, ContextChange{
			Agent: agent, BeforeChars: before, AfterChars: after, SavedChars: saved, EstimatedTokensSaved: saved / 4,
		})
	}

	sort.Slice(plan.Operations, func(i, j int) bool {
		a, b := plan.Operations[i], plan.Operations[j]
		return a.Agent+a.Path+a.Action < b.Agent+b.Path+b.Action
	})
	sort.Slice(plan.Conflicts, func(i, j int) bool {
		a, b := plan.Conflicts[i], plan.Conflicts[j]
		return a.Agent+a.Path < b.Agent+b.Path
	})
	return plan, nil
}

func Apply(cfg config.File, plan Plan) (ApplyResult, error) {
	if len(plan.Conflicts) > 0 {
		return ApplyResult{}, fmt.Errorf("plan has %d conflict(s); resolve them before apply", len(plan.Conflicts))
	}
	if len(plan.Operations) == 0 {
		return ApplyResult{Noop: true}, nil
	}

	for _, op := range plan.Operations {
		if op.Action == "write_router" || op.Action == "write_proxy" {
			if err := writeGenerated(op.Path, op.content, op.policy); err != nil {
				return ApplyResult{}, err
			}
		}
	}

	snap := snapshot{CreatedAt: time.Now().UTC()}
	for _, op := range plan.Operations {
		if op.Action != "create_link" && op.Action != "replace_link" && op.Action != "remove_link" {
			continue
		}
		entry, err := capture(op.Path, op.Target)
		if err != nil {
			return ApplyResult{}, err
		}
		snap.Entries = append(snap.Entries, entry)
	}

	completed := 0
	for _, op := range plan.Operations {
		if op.Action != "create_link" && op.Action != "replace_link" && op.Action != "remove_link" {
			completed++
			continue
		}
		if err := applyLink(op); err != nil {
			_ = restore(snap, false)
			return ApplyResult{}, err
		}
		completed++
	}

	snapshotPath := filepath.Join(cfg.Settings.StateDir, "latest.json")
	resultSnapshot := ""
	if len(snap.Entries) > 0 {
		if err := writeJSON(snapshotPath, snap); err != nil {
			_ = restore(snap, false)
			return ApplyResult{}, err
		}
		resultSnapshot = snapshotPath
	}
	return ApplyResult{Applied: completed, Snapshot: resultSnapshot}, nil
}

func Rollback(cfg config.File) (RollbackResult, error) {
	path := filepath.Join(cfg.Settings.StateDir, "latest.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RollbackResult{Noop: true}, nil
	}
	if err != nil {
		return RollbackResult{}, fmt.Errorf("read rollback snapshot: %w", err)
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return RollbackResult{}, fmt.Errorf("parse rollback snapshot: %w", err)
	}
	if err := restore(snap, true); err != nil {
		return RollbackResult{}, err
	}
	if err := os.Remove(path); err != nil {
		return RollbackResult{}, fmt.Errorf("remove rollback snapshot: %w", err)
	}
	return RollbackResult{Restored: len(snap.Entries)}, nil
}

func planLink(plan *Plan, agent, path, target, reason string) {
	canonicalTarget, err := filepath.Abs(target)
	if err != nil {
		plan.Conflicts = append(plan.Conflicts, Conflict{Agent: agent, Path: path, Message: err.Error()})
		return
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		plan.Operations = append(plan.Operations, Operation{Action: "create_link", Agent: agent, Path: path, Target: canonicalTarget, Reason: reason})
		return
	}
	if err != nil {
		plan.Conflicts = append(plan.Conflicts, Conflict{Agent: agent, Path: path, Message: err.Error()})
		return
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if samePath(path, canonicalTarget) {
			return
		}
		plan.Conflicts = append(plan.Conflicts, Conflict{Agent: agent, Path: path, Message: "refusing to replace a real file or directory"})
		return
	}
	current, err := filepath.EvalSymlinks(path)
	if err == nil && samePath(current, canonicalTarget) {
		return
	}
	plan.Operations = append(plan.Operations, Operation{Action: "replace_link", Agent: agent, Path: path, Target: canonicalTarget, Reason: reason})
}

func planAbsent(plan *Plan, agent, path, reason string) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		plan.Conflicts = append(plan.Conflicts, Conflict{Agent: agent, Path: path, Message: err.Error()})
		return
	}
	if info.Mode()&os.ModeSymlink == 0 {
		plan.Conflicts = append(plan.Conflicts, Conflict{Agent: agent, Path: path, Message: "refusing to remove a real file or directory"})
		return
	}
	plan.Operations = append(plan.Operations, Operation{Action: "remove_link", Agent: agent, Path: path, Reason: reason})
}

func planGenerated(plan *Plan, agent, path, action, content, policy, reason string) {
	info, statErr := os.Lstat(path)
	if statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			plan.Conflicts = append(plan.Conflicts, Conflict{Agent: agent, Path: path, Message: "refusing to replace unowned generated path"})
			return
		}
		if _, err := os.Stat(filepath.Join(path, generatedMarker)); err != nil {
			plan.Conflicts = append(plan.Conflicts, Conflict{Agent: agent, Path: path, Message: "generated directory lacks SkillTrim ownership marker"})
			return
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		plan.Conflicts = append(plan.Conflicts, Conflict{Agent: agent, Path: path, Message: statErr.Error()})
		return
	}
	current, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
	policyCurrent, policyErr := os.ReadFile(filepath.Join(path, "agents", "openai.yaml"))
	policyChanged := policy != "" && (policyErr != nil || string(policyCurrent) != policy)
	policyStale := policy == "" && policyErr == nil
	if err == nil && string(current) == content && !policyChanged && !policyStale {
		return
	}
	plan.Operations = append(plan.Operations, Operation{Action: action, Agent: agent, Path: path, Reason: reason, content: content, policy: policy})
}

func removeStaleGenerated(plan *Plan, agent, targetDir, dataDir string, desired map[string]bool) {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return
	}
	generatedRoot := filepath.Clean(dataDir) + string(os.PathSeparator)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		path := filepath.Join(targetDir, entry.Name())
		target, err := resolvedLink(path)
		if err != nil || !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), generatedRoot) || desired[target] {
			continue
		}
		plan.Operations = append(plan.Operations, Operation{Action: "remove_link", Agent: agent, Path: path, Reason: "remove stale SkillTrim-generated activation"})
	}
}

func proxyContent(agent string, skill core.Skill) (string, string) {
	disable := ""
	if agent == "claude" {
		disable = "disable-model-invocation: true\n"
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: Explicit loader for %s.\n%s---\n\n# %s\n\nRead `%s` completely, then follow it for this request.\n", skill.Name, skill.Name, disable, skill.Name, filepath.Join(skill.Source, "SKILL.md"))
	policy := ""
	if agent == "codex" {
		policy = "policy:\n  allow_implicit_invocation: false\n"
	}
	return content, policy
}

func routerContent(agent string, group core.Group, skills []core.Skill) (string, string) {
	description := group.Description
	if description == "" {
		description = fmt.Sprintf("Explicit router for %s skills.", group.Name)
	}
	disable := ""
	if agent == "claude" {
		disable = "disable-model-invocation: true\n"
	}
	var body strings.Builder
	fmt.Fprintf(&body, "---\nname: %s\ndescription: %s\n%s---\n\n# %s router\n\nChoose one routed skill matching request. Read its complete `SKILL.md`, then follow it. If none match, say so.\n\n## Routed skills\n\n", group.Name, yamlQuote(description), disable, group.Name)
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	for _, skill := range skills {
		fmt.Fprintf(&body, "- `%s`: %s (`%s`)\n", skill.Name, oneLine(skill.Description), filepath.Join(skill.Source, "SKILL.md"))
	}
	policy := ""
	if agent == "codex" {
		policy = "policy:\n  allow_implicit_invocation: false\n"
	}
	return body.String(), policy
}

func writeGenerated(path, content, policy string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("refusing to replace unowned generated path %s", path)
		}
		if _, err := os.Stat(filepath.Join(path, generatedMarker)); err != nil {
			return fmt.Errorf("generated directory lacks SkillTrim ownership marker: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect generated skill %s: %w", path, err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create generated skill %s: %w", path, err)
	}
	if err := os.WriteFile(filepath.Join(path, generatedMarker), []byte("skilltrim\n"), 0o644); err != nil {
		return fmt.Errorf("mark generated skill %s: %w", path, err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(content), 0o644); err != nil {
		return fmt.Errorf("write generated skill %s: %w", path, err)
	}
	policyDir := filepath.Join(path, "agents")
	policyPath := filepath.Join(policyDir, "openai.yaml")
	if policy == "" {
		if err := os.Remove(policyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale generated policy: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		return fmt.Errorf("create generated policy directory: %w", err)
	}
	if err := os.WriteFile(policyPath, []byte(policy), 0o644); err != nil {
		return fmt.Errorf("write generated policy: %w", err)
	}
	return nil
}

func applyLink(op Operation) error {
	if err := os.MkdirAll(filepath.Dir(op.Path), 0o755); err != nil {
		return fmt.Errorf("create skills directory: %w", err)
	}
	if op.Action == "replace_link" || op.Action == "remove_link" {
		if err := removeLink(op.Path); err != nil {
			return fmt.Errorf("remove %s: %w", op.Path, err)
		}
	}
	if op.Action == "create_link" || op.Action == "replace_link" {
		if err := os.Symlink(op.Target, op.Path); err != nil {
			return fmt.Errorf("link %s: %w", op.Path, err)
		}
	}
	return nil
}

func removeLink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return errors.New("path is no longer a symlink")
	}
	return os.Remove(path)
}

func capture(path, afterTarget string) (snapshotEntry, error) {
	entry := snapshotEntry{Path: path, BeforeKind: "absent", AfterTarget: afterTarget}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return entry, nil
	}
	if err != nil {
		return entry, fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return entry, fmt.Errorf("refusing to snapshot non-symlink %s", path)
	}
	target, err := resolvedLink(path)
	if err != nil {
		return entry, fmt.Errorf("read symlink %s: %w", path, err)
	}
	entry.BeforeKind = "symlink"
	entry.BeforeTarget = target
	return entry, nil
}

func restore(snap snapshot, verify bool) error {
	for i := len(snap.Entries) - 1; i >= 0; i-- {
		entry := snap.Entries[i]
		info, err := os.Lstat(entry.Path)
		exists := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s during rollback: %w", entry.Path, err)
		}
		if verify {
			if entry.AfterTarget == "" && exists {
				return fmt.Errorf("refusing rollback: %s changed after apply", entry.Path)
			}
			if entry.AfterTarget != "" {
				if !exists || info.Mode()&os.ModeSymlink == 0 {
					return fmt.Errorf("refusing rollback: %s changed after apply", entry.Path)
				}
				current, linkErr := resolvedLink(entry.Path)
				if linkErr != nil || !samePath(current, entry.AfterTarget) {
					return fmt.Errorf("refusing rollback: %s changed after apply", entry.Path)
				}
			}
		}
		if exists {
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("refusing to replace non-symlink %s during rollback", entry.Path)
			}
			if err := os.Remove(entry.Path); err != nil {
				return fmt.Errorf("remove %s during rollback: %w", entry.Path, err)
			}
		}
		if entry.BeforeKind == "symlink" {
			if err := os.MkdirAll(filepath.Dir(entry.Path), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(entry.BeforeTarget, entry.Path); err != nil {
				return fmt.Errorf("restore %s: %w", entry.Path, err)
			}
		}
	}
	return nil
}

func selectedAgents(cfg config.File, selected string) ([]string, error) {
	if selected == "" || selected == "all" {
		return cfg.AgentNames(), nil
	}
	if _, ok := cfg.Agents[selected]; !ok {
		return nil, fmt.Errorf("unknown agent %q", selected)
	}
	return []string{selected}, nil
}

func resolvedLink(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target), nil
}

func samePath(a, b string) bool {
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && filepath.Clean(absA) == filepath.Clean(absB)
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func agentContext(cat core.Catalog, agent string) int {
	for _, summary := range cat.Agents {
		if summary.Name == agent {
			return summary.ContextChars
		}
	}
	return 0
}

func contextChars(name, description, path string) int {
	return utf8.RuneCountInString(name) + utf8.RuneCountInString(description) + utf8.RuneCountInString(path)
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func yamlQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".snapshot-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func CurrentCatalog(cfg config.File, project string) (core.Catalog, error) {
	return catalog.Scan(cfg, project)
}
