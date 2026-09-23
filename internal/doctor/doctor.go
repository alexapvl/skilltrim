package doctor

import (
	"fmt"
	"sort"

	"github.com/alexapvl/skilltrim/internal/catalog"
	"github.com/alexapvl/skilltrim/internal/config"
	"github.com/alexapvl/skilltrim/internal/core"
	"github.com/alexapvl/skilltrim/internal/engine"
)

type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type Result struct {
	Status string  `json:"status"`
	Issues []Issue `json:"issues"`
}

func Check(cfg config.File, selectedAgent, project string) (Result, error) {
	cat, err := catalog.Scan(cfg, project)
	if err != nil {
		return Result{}, err
	}
	warnings := catalog.WarningsForAgent(cat.Warnings, cfg, selectedAgent, project)
	issues := make([]Issue, 0, len(warnings))
	for _, warning := range warnings {
		issues = append(issues, Issue{Code: warning.Code, Message: warning.Message, Path: warning.Path})
	}
	known := map[string]bool{}
	for _, skill := range cat.Skills {
		known[skill.Name] = true
	}
	scope := core.Scope(project)
	for _, rule := range cfg.Rules {
		if rule.Scope != scope || (selectedAgent != "" && selectedAgent != "all" && rule.Agent != selectedAgent) {
			continue
		}
		if !known[rule.Skill] {
			issues = append(issues, Issue{Code: "missing_skill", Message: fmt.Sprintf("rule references missing skill %q", rule.Skill)})
		}
		if _, ok := cfg.Agents[rule.Agent]; !ok {
			issues = append(issues, Issue{Code: "unknown_agent", Message: fmt.Sprintf("rule references unknown agent %q", rule.Agent)})
		}
		if rule.Mode.IsGroup() {
			if _, ok := cfg.Group(rule.Mode.Group()); !ok {
				issues = append(issues, Issue{Code: "missing_group", Message: fmt.Sprintf("rule references missing group %q", rule.Mode.Group())})
			}
		}
	}
	for _, group := range cfg.Groups {
		for _, member := range group.Members {
			if !known[member] {
				issues = append(issues, Issue{Code: "missing_group_member", Message: fmt.Sprintf("group %q references missing skill %q", group.Name, member)})
			}
		}
	}
	plan, err := engine.BuildPlan(cfg, cat, selectedAgent, project)
	if err != nil {
		return Result{}, err
	}
	for _, conflict := range plan.Conflicts {
		issues = append(issues, Issue{Code: "plan_conflict", Message: conflict.Message, Path: conflict.Path})
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Code+issues[i].Path < issues[j].Code+issues[j].Path })
	status := "ok"
	if len(issues) > 0 {
		status = "issues"
	}
	return Result{Status: status, Issues: issues}, nil
}

func TargetMode(skill core.Skill, agent string) core.Mode {
	for _, target := range skill.Targets {
		if target.Agent == agent {
			return target.Mode
		}
	}
	return core.ModeOff
}
