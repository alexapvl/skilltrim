package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alexapvl/skilltrim/internal/catalog"
	"github.com/alexapvl/skilltrim/internal/config"
	"github.com/alexapvl/skilltrim/internal/core"
	"github.com/alexapvl/skilltrim/internal/doctor"
	"github.com/alexapvl/skilltrim/internal/engine"
	"github.com/alexapvl/skilltrim/internal/output"
	"github.com/alexapvl/skilltrim/internal/tui"
)

type options struct {
	toon       bool
	agent      string
	project    string
	configPath string
	help       bool
}

type dashboard struct {
	Bin         string              `json:"bin"`
	Description string              `json:"description"`
	Scope       string              `json:"scope"`
	Skills      int                 `json:"skills"`
	Groups      int                 `json:"groups"`
	Agents      []core.AgentSummary `json:"agents"`
	Warnings    int                 `json:"warnings"`
	Help        []string            `json:"help"`
}

type listItem struct {
	Name         string    `json:"name"`
	Mode         core.Mode `json:"mode,omitempty"`
	ActiveAgents []string  `json:"active_agents,omitempty"`
	ContextChars int       `json:"context_chars"`
	Source       string    `json:"source"`
	Description  string    `json:"description,omitempty"`
}

type listResult struct {
	Count  int        `json:"count"`
	Scope  string     `json:"scope"`
	Skills []listItem `json:"skills"`
	Help   []string   `json:"help,omitempty"`
}

type mutationResult struct {
	Changed int       `json:"changed"`
	Noop    bool      `json:"noop"`
	Skills  []string  `json:"skills,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	Scope   string    `json:"scope,omitempty"`
	Mode    core.Mode `json:"mode,omitempty"`
	Group   string    `json:"group,omitempty"`
	Help    []string  `json:"help,omitempty"`
}

type moveResult struct {
	Status      string              `json:"status"`
	Skills      []string            `json:"skills"`
	Agents      []string            `json:"agents"`
	Projects    []string            `json:"projects"`
	Plans       []engine.Plan       `json:"plans"`
	FileChanges []engine.FileChange `json:"file_changes"`
	Applied     int                 `json:"applied,omitempty"`
	Snapshot    string              `json:"snapshot,omitempty"`
	Help        []string            `json:"help,omitempty"`
}

func Run(args []string, stdout, stderr io.Writer) int {
	_ = stderr
	if len(args) == 1 && (args[0] == "-v" || args[0] == "-V" || args[0] == "--version") {
		fmt.Fprintln(stdout, core.Version)
		return 0
	}
	opts, rest, err := parseOptions(args)
	if err != nil {
		return writeError(stdout, opts.toon, err, []string{"Valid global flags: --agent, --project, --config, --toon, --help, --version."}, 2)
	}
	if opts.help && len(rest) == 0 {
		fmt.Fprint(stdout, topHelp)
		return 0
	}
	if len(rest) > 0 && rest[0] == "help" {
		if len(rest) == 1 {
			fmt.Fprint(stdout, topHelp)
		} else {
			fmt.Fprint(stdout, commandHelp(rest[1]))
		}
		return 0
	}

	home, err := homeDir()
	if err != nil {
		return writeError(stdout, opts.toon, err, nil, 1)
	}
	if opts.configPath == "" {
		opts.configPath = config.DefaultPath(home)
	} else {
		opts.configPath = config.Expand(opts.configPath, home)
	}
	cfg, err := config.Load(opts.configPath, home)
	if err != nil {
		return writeError(stdout, opts.toon, err, []string{"Fix config or pass `--config <path>`."}, 1)
	}

	if len(rest) == 0 {
		return runDashboard(stdout, opts, cfg)
	}
	command, commandArgs := rest[0], rest[1:]
	if opts.help {
		fmt.Fprint(stdout, commandHelp(command))
		return 0
	}
	switch command {
	case "list":
		return runList(stdout, opts, cfg, commandArgs)
	case "mode":
		return runMode(stdout, opts, cfg, commandArgs)
	case "group":
		return runGroup(stdout, opts, cfg, commandArgs)
	case "move":
		return runMove(stdout, opts, cfg, commandArgs)
	case "plan":
		return runPlan(stdout, opts, cfg, commandArgs)
	case "apply":
		return runApply(stdout, opts, cfg, commandArgs)
	case "rollback":
		return runRollback(stdout, opts, cfg, commandArgs)
	case "doctor":
		return runDoctor(stdout, opts, cfg, commandArgs)
	case "tui":
		return runTUI(stdout, opts, cfg, commandArgs)
	default:
		return writeError(stdout, opts.toon, fmt.Errorf("unknown command %q", command), []string{"Valid commands: list, mode, group, move, plan, apply, rollback, doctor, tui."}, 2)
	}
}

func runDashboard(writer io.Writer, opts options, cfg config.File) int {
	cat, err := catalog.Scan(cfg, opts.project)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	agents, err := filterAgents(cat.Agents, opts.agent)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 2)
	}
	executable, _ := os.Executable()
	result := dashboard{
		Bin: collapseHome(executable), Description: "Control which agent skills enter model context",
		Scope: core.Scope(opts.project), Skills: len(cat.Skills), Groups: len(cfg.Groups), Agents: agents,
		Warnings: len(catalog.WarningsForAgent(cat.Warnings, cfg, opts.agent, opts.project)),
		Help:     []string{"Run `skilltrim list` to inspect skills.", "Run `skilltrim plan` to preview configured changes.", "Run `skilltrim tui` for interactive management."},
	}
	if err := output.Write(writer, result, opts.toon); err != nil {
		return 1
	}
	return 0
}

func runList(writer io.Writer, opts options, cfg config.File, args []string) int {
	full := false
	for _, arg := range args {
		if arg == "--full" {
			full = true
		} else {
			return writeError(writer, opts.toon, fmt.Errorf("unknown argument %q for `list`", arg), []string{"Valid flags: --full, --agent, --project, --config, --toon."}, 2)
		}
	}
	if opts.agent != "" && opts.agent != "all" {
		if _, ok := cfg.Agents[opts.agent]; !ok {
			return writeError(writer, opts.toon, fmt.Errorf("unknown agent %q", opts.agent), nil, 2)
		}
	}
	cat, err := catalog.Scan(cfg, opts.project)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	items := make([]listItem, 0, len(cat.Skills))
	for _, skill := range cat.Skills {
		item := listItem{Name: skill.Name, ActiveAgents: skill.ActiveAgents, ContextChars: skill.ContextChars, Source: skill.Source}
		if opts.agent != "" && opts.agent != "all" {
			item.Mode = doctor.TargetMode(skill, opts.agent)
		}
		if full {
			item.Description = skill.Description
		}
		items = append(items, item)
	}
	result := listResult{Count: len(items), Scope: core.Scope(opts.project), Skills: items}
	if len(items) > 0 {
		result.Help = []string{"Run `skilltrim list --full` for descriptions.", "Run `skilltrim mode set <skill> <mode> --agent <agent>` to stage a change."}
	}
	if err := output.Write(writer, result, opts.toon); err != nil {
		return 1
	}
	return 0
}

func runMode(writer io.Writer, opts options, cfg config.File, args []string) int {
	if len(args) > 0 && args[0] == "--help" {
		fmt.Fprint(writer, modeHelp)
		return 0
	}
	if len(args) != 3 || args[0] != "set" {
		return writeError(writer, opts.toon, errors.New("usage: skilltrim mode set <skill-or-glob> <mode> --agent <agent>"), []string{"Modes: auto, explicit, group:<name>, off."}, 2)
	}
	if opts.agent == "" || opts.agent == "all" {
		return writeError(writer, opts.toon, errors.New("--agent is required for `mode set`"), []string{"Example: skilltrim mode set 'asc-*' group:asc --agent codex"}, 2)
	}
	if _, ok := cfg.Agents[opts.agent]; !ok {
		return writeError(writer, opts.toon, fmt.Errorf("unknown agent %q", opts.agent), nil, 2)
	}
	mode, err := core.ParseMode(args[2])
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 2)
	}
	group := core.Group{}
	if mode.IsGroup() {
		var ok bool
		group, ok = cfg.Group(mode.Group())
		if !ok {
			return writeError(writer, opts.toon, fmt.Errorf("group %q does not exist", mode.Group()), []string{fmt.Sprintf("Run `skilltrim group create %s` first.", mode.Group())}, 1)
		}
	}
	cat, err := catalog.Scan(cfg, opts.project)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	matches, err := catalog.Match(cat, args[1])
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	scope := core.Scope(opts.project)
	changed := 0
	names := make([]string, 0, len(matches))
	for _, skill := range matches {
		oldMode, configured := cfg.RuleMode(skill.Name, opts.agent, scope)
		if !configured || oldMode != mode {
			changed++
		}
		cfg.SetMode(skill.Name, opts.agent, scope, mode)
		cfg.AddSource(filepath.Dir(skill.Source))
		names = append(names, skill.Name)
		if mode.IsGroup() && !contains(group.Members, skill.Name) {
			group.Members = append(group.Members, skill.Name)
		}
	}
	if mode.IsGroup() {
		sort.Strings(group.Members)
		cfg.SetGroup(group)
	}
	if err := config.Save(opts.configPath, cfg); err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	result := mutationResult{Changed: changed, Noop: changed == 0, Skills: names, Agent: opts.agent, Scope: scope, Mode: mode, Help: []string{"Run `skilltrim plan` to preview filesystem changes."}}
	if err := output.Write(writer, result, opts.toon); err != nil {
		return 1
	}
	return 0
}

func runGroup(writer io.Writer, opts options, cfg config.File, args []string) int {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Fprint(writer, groupHelp)
		return 0
	}
	switch args[0] {
	case "create":
		if len(args) != 2 || !core.ValidName(args[1]) {
			return writeError(writer, opts.toon, errors.New("usage: skilltrim group create <name>"), nil, 2)
		}
		if _, exists := cfg.Group(args[1]); exists {
			return writeValue(writer, opts.toon, mutationResult{Noop: true, Group: args[1]})
		}
		cfg.SetGroup(core.Group{Name: args[1], Members: []string{}})
		if err := config.Save(opts.configPath, cfg); err != nil {
			return writeError(writer, opts.toon, err, nil, 1)
		}
		return writeValue(writer, opts.toon, mutationResult{Changed: 1, Group: args[1], Help: []string{fmt.Sprintf("Run `skilltrim group add %s <skill-or-glob>`.", args[1])}})
	case "add":
		return groupAdd(writer, opts, cfg, args[1:])
	case "remove":
		return groupRemove(writer, opts, cfg, args[1:])
	case "list":
		if len(args) != 1 {
			return writeError(writer, opts.toon, errors.New("usage: skilltrim group list"), nil, 2)
		}
		return writeValue(writer, opts.toon, struct {
			Count  int          `json:"count"`
			Groups []core.Group `json:"groups"`
		}{Count: len(cfg.Groups), Groups: cfg.Groups})
	case "delete":
		return groupDelete(writer, opts, cfg, args[1:])
	default:
		return writeError(writer, opts.toon, fmt.Errorf("unknown group command %q", args[0]), []string{"Valid commands: create, add, remove, list, delete."}, 2)
	}
}

func groupAdd(writer io.Writer, opts options, cfg config.File, args []string) int {
	if len(args) < 2 {
		return writeError(writer, opts.toon, errors.New("usage: skilltrim group add <name> <skill-or-glob>..."), nil, 2)
	}
	group, ok := cfg.Group(args[0])
	if !ok {
		return writeError(writer, opts.toon, fmt.Errorf("group %q does not exist", args[0]), nil, 1)
	}
	if opts.agent != "" && opts.agent != "all" {
		if _, ok := cfg.Agents[opts.agent]; !ok {
			return writeError(writer, opts.toon, fmt.Errorf("unknown agent %q", opts.agent), nil, 2)
		}
	}
	cat, err := catalog.Scan(cfg, opts.project)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	matched, err := matchPatterns(cat, args[1:])
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	changed := 0
	for _, skill := range matched {
		cfg.AddSource(filepath.Dir(skill.Source))
		if !contains(group.Members, skill.Name) {
			group.Members = append(group.Members, skill.Name)
			changed++
		}
		if opts.agent != "" && opts.agent != "all" {
			cfg.SetMode(skill.Name, opts.agent, core.Scope(opts.project), core.Mode("group:"+group.Name))
		}
	}
	sort.Strings(group.Members)
	cfg.SetGroup(group)
	if err := config.Save(opts.configPath, cfg); err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	return writeValue(writer, opts.toon, mutationResult{Changed: changed, Noop: changed == 0, Skills: skillNames(matched), Group: group.Name, Agent: opts.agent, Help: []string{"Pass `--agent <agent>` to assign group mode while adding.", "Run `skilltrim plan` to preview filesystem changes."}})
}

func groupRemove(writer io.Writer, opts options, cfg config.File, args []string) int {
	if len(args) < 2 {
		return writeError(writer, opts.toon, errors.New("usage: skilltrim group remove <name> <skill-or-glob>..."), nil, 2)
	}
	group, ok := cfg.Group(args[0])
	if !ok {
		return writeError(writer, opts.toon, fmt.Errorf("group %q does not exist", args[0]), nil, 1)
	}
	remove := map[string]bool{}
	for _, pattern := range args[1:] {
		matched := false
		for _, member := range group.Members {
			ok, err := filepath.Match(pattern, member)
			if err != nil {
				return writeError(writer, opts.toon, fmt.Errorf("invalid skill pattern %q: %w", pattern, err), nil, 2)
			}
			if ok {
				remove[member], matched = true, true
			}
		}
		if !matched {
			return writeError(writer, opts.toon, fmt.Errorf("no members match %q", pattern), nil, 1)
		}
	}
	kept := group.Members[:0]
	for _, member := range group.Members {
		if !remove[member] {
			kept = append(kept, member)
		}
	}
	group.Members = kept
	for i := range cfg.Rules {
		if remove[cfg.Rules[i].Skill] && cfg.Rules[i].Mode == core.Mode("group:"+group.Name) && (opts.agent == "" || opts.agent == "all" || cfg.Rules[i].Agent == opts.agent) {
			cfg.Rules[i].Mode = core.ModeOff
		}
	}
	cfg.SetGroup(group)
	if err := config.Save(opts.configPath, cfg); err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	names := sortedMapKeys(remove)
	return writeValue(writer, opts.toon, mutationResult{Changed: len(names), Skills: names, Group: group.Name, Agent: opts.agent, Help: []string{"Removed grouped skills become off for affected agents. Run `skilltrim plan`."}})
}

func groupDelete(writer io.Writer, opts options, cfg config.File, args []string) int {
	force := false
	var positional []string
	for _, arg := range args {
		if arg == "--force" {
			force = true
		} else {
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return writeError(writer, opts.toon, errors.New("usage: skilltrim group delete <name> [--force]"), nil, 2)
	}
	group, ok := cfg.Group(positional[0])
	if !ok {
		return writeValue(writer, opts.toon, mutationResult{Noop: true, Group: positional[0]})
	}
	if len(group.Members) > 0 && !force {
		return writeError(writer, opts.toon, fmt.Errorf("group %q has %d member(s)", group.Name, len(group.Members)), []string{fmt.Sprintf("Run `skilltrim group delete %s --force` to disable members and delete it.", group.Name)}, 1)
	}
	for i := range cfg.Rules {
		if cfg.Rules[i].Mode == core.Mode("group:"+group.Name) {
			cfg.Rules[i].Mode = core.ModeOff
		}
	}
	cfg.DeleteGroup(group.Name)
	if err := config.Save(opts.configPath, cfg); err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	return writeValue(writer, opts.toon, mutationResult{Changed: 1, Group: group.Name, Help: []string{"Affected grouped skills are now off. Run `skilltrim plan`."}})
}

func runMove(writer io.Writer, opts options, cfg config.File, args []string) int {
	if len(args) > 0 && args[0] == "--help" {
		fmt.Fprint(writer, moveHelp)
		return 0
	}
	if opts.project != "" {
		return writeError(writer, opts.toon, errors.New("`move` uses --to-project and does not accept --project"), []string{"Run `skilltrim move --help`."}, 2)
	}
	if opts.agent == "" {
		return writeError(writer, opts.toon, errors.New("--agent is required for `move`"), []string{"Use --agent <name> or --agent all."}, 2)
	}
	if opts.agent != "all" {
		if _, ok := cfg.Agents[opts.agent]; !ok {
			return writeError(writer, opts.toon, fmt.Errorf("unknown agent %q", opts.agent), nil, 2)
		}
	}

	pattern, projects, applyNow, err := parseMoveArgs(args)
	if err != nil {
		return writeError(writer, opts.toon, err, []string{"Run `skilltrim move --help`."}, 2)
	}
	home, err := homeDir()
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	for i, project := range projects {
		projects[i] = config.Expand(project, home)
		abs, err := filepath.Abs(projects[i])
		if err != nil {
			return writeError(writer, opts.toon, fmt.Errorf("resolve project %q: %w", projects[i], err), nil, 1)
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return writeError(writer, opts.toon, fmt.Errorf("project is not a directory: %s", abs), nil, 1)
		}
		projects[i] = filepath.Clean(abs)
	}
	projects = uniqueSorted(projects)

	globalCatalog, err := catalog.Scan(cfg, "")
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	matches, err := catalog.Match(globalCatalog, pattern)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}

	next := cloneConfig(cfg)
	agentSet := map[string]bool{}
	assignments := map[string][]string{}
	for _, skill := range matches {
		if _, grouped := cfg.Group(skill.Name); grouped {
			return writeError(writer, opts.toon, fmt.Errorf("%q is a generated group router", skill.Name), []string{"Move individual skills instead of the router."}, 1)
		}
		agents := moveAgents(cfg, skill, projects, opts.agent)
		if len(agents) == 0 {
			return writeError(writer, opts.toon, fmt.Errorf("skill %q is not globally active for selected agents", skill.Name), []string{"Choose an active agent or use `skilltrim list --agent <name>`."}, 1)
		}
		assignments[skill.Name] = agents
		next.AddSource(filepath.Dir(skill.Source))
		for _, agent := range agents {
			agentSet[agent] = true
			next.SetMode(skill.Name, agent, "global", core.ModeOff)
			for _, project := range projects {
				next.SetMode(skill.Name, agent, core.Scope(project), core.ModeAuto)
			}
		}
	}
	agents := sortedMapKeys(agentSet)

	for _, project := range append([]string{""}, projects...) {
		plan, err := buildScopePlan(cfg, agents, project)
		if err != nil {
			return writeError(writer, opts.toon, err, nil, 1)
		}
		if len(plan.Operations) > 0 || len(plan.Conflicts) > 0 {
			return writeError(writer, opts.toon, errors.New("existing unapplied changes must be resolved before `move`"), []string{"Run `skilltrim plan` for global scope and each target project."}, 1)
		}
	}

	plans := make([]engine.Plan, 0, len(projects)+1)
	globalPlan, err := buildScopePlan(next, agents, "")
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	plans = append(plans, globalPlan)
	for _, project := range projects {
		plan, err := buildScopePlan(next, agents, project)
		if err != nil {
			return writeError(writer, opts.toon, err, nil, 1)
		}
		plans = append(plans, plan)
	}

	gitPaths := make(map[string][]string, len(projects))
	for _, project := range projects {
		paths := map[string]bool{}
		for _, skill := range matches {
			for _, agent := range assignments[skill.Name] {
				targetDir, err := next.TargetDir(agent, project)
				if err != nil {
					return writeError(writer, opts.toon, err, nil, 1)
				}
				paths[filepath.Join(targetDir, skill.Name)] = true
			}
		}
		gitPaths[project] = sortedMapKeys(paths)
	}
	files, err := engine.PlanGitExcludes(gitPaths)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	encoded, err := config.Encode(next)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	configChange, changed, err := engine.PlanFileChange(opts.configPath, encoded, "update skilltrim configuration", 0o600)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	if changed {
		files = append(files, configChange)
	}

	result := moveResult{
		Status: "preview", Skills: skillNames(matches), Agents: agents, Projects: projects,
		Plans: plans, FileChanges: files, Help: []string{"Add --apply to perform this move."},
	}
	for _, plan := range plans {
		if len(plan.Conflicts) > 0 {
			result.Help = []string{"Resolve conflicts before applying."}
			writeValue(writer, opts.toon, result)
			return 1
		}
	}
	if !applyNow {
		return writeValue(writer, opts.toon, result)
	}
	applyResult, err := engine.ApplyMany(cfg, plans, files)
	if err != nil {
		return writeError(writer, opts.toon, err, []string{"No move was committed. Resolve the error and preview again."}, 1)
	}
	if applyResult.Noop {
		result.Status = "noop"
		result.Help = nil
	} else {
		result.Status = "applied"
		result.Applied = applyResult.Applied
		result.Snapshot = applyResult.Snapshot
		result.Help = []string{"Run `skilltrim rollback` to restore the complete move."}
	}
	return writeValue(writer, opts.toon, result)
}

func parseMoveArgs(args []string) (string, []string, bool, error) {
	var pattern string
	var projects []string
	applyNow := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--apply":
			applyNow = true
		case args[i] == "--to-project":
			if i+1 >= len(args) {
				return "", nil, false, errors.New("--to-project requires a value")
			}
			i++
			projects = append(projects, args[i])
		case strings.HasPrefix(args[i], "--to-project="):
			projects = append(projects, strings.TrimPrefix(args[i], "--to-project="))
		case strings.HasPrefix(args[i], "-"):
			return "", nil, false, fmt.Errorf("unknown flag %q for `move`", args[i])
		case pattern == "":
			pattern = args[i]
		default:
			return "", nil, false, fmt.Errorf("unexpected argument %q for `move`", args[i])
		}
	}
	if pattern == "" || len(projects) == 0 {
		return "", nil, false, errors.New("usage: skilltrim move <skill-or-glob> --to-project <path> [--to-project <path>...] --agent <name|all> [--apply]")
	}
	for _, project := range projects {
		if strings.TrimSpace(project) == "" {
			return "", nil, false, errors.New("--to-project requires a non-empty path")
		}
	}
	return pattern, projects, applyNow, nil
}

func buildScopePlan(cfg config.File, agents []string, project string) (engine.Plan, error) {
	cat, err := catalog.Scan(cfg, project)
	if err != nil {
		return engine.Plan{}, err
	}
	combined := engine.Plan{Scope: core.Scope(project), Context: []engine.ContextChange{}, Operations: []engine.Operation{}, Conflicts: []engine.Conflict{}}
	for _, agent := range agents {
		plan, err := engine.BuildPlan(cfg, cat, agent, project)
		if err != nil {
			return engine.Plan{}, err
		}
		combined.Context = append(combined.Context, plan.Context...)
		combined.Operations = append(combined.Operations, plan.Operations...)
		combined.Conflicts = append(combined.Conflicts, plan.Conflicts...)
	}
	return combined, nil
}

func moveAgents(cfg config.File, skill core.Skill, projects []string, selected string) []string {
	candidates := map[string]bool{}
	for _, agent := range skill.ActiveAgents {
		candidates[agent] = true
	}
	for _, rule := range cfg.Rules {
		if rule.Skill != skill.Name {
			continue
		}
		for _, project := range projects {
			if rule.Scope == core.Scope(project) {
				candidates[rule.Agent] = true
			}
		}
	}
	if selected != "all" {
		if candidates[selected] {
			return []string{selected}
		}
		return nil
	}
	return sortedMapKeys(candidates)
}

func cloneConfig(cfg config.File) config.File {
	result := cfg
	result.Sources = append([]string(nil), cfg.Sources...)
	result.Rules = append([]core.Rule(nil), cfg.Rules...)
	result.Groups = append([]core.Group(nil), cfg.Groups...)
	for i := range result.Groups {
		result.Groups[i].Members = append([]string(nil), result.Groups[i].Members...)
	}
	result.Agents = make(map[string]core.Agent, len(cfg.Agents))
	for name, agent := range cfg.Agents {
		result.Agents[name] = agent
	}
	return result
}

func uniqueSorted(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	return sortedMapKeys(set)
}

func runPlan(writer io.Writer, opts options, cfg config.File, args []string) int {
	if len(args) != 0 {
		return writeError(writer, opts.toon, fmt.Errorf("unknown argument %q for `plan`", args[0]), []string{"Valid flags: --agent, --project, --config, --toon."}, 2)
	}
	cat, err := catalog.Scan(cfg, opts.project)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	plan, err := engine.BuildPlan(cfg, cat, opts.agent, opts.project)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 2)
	}
	code := writeValue(writer, opts.toon, plan)
	if code == 0 && len(plan.Conflicts) > 0 {
		return 1
	}
	return code
}

func runApply(writer io.Writer, opts options, cfg config.File, args []string) int {
	if len(args) != 0 {
		return writeError(writer, opts.toon, fmt.Errorf("unknown argument %q for `apply`", args[0]), []string{"Run `skilltrim plan` first."}, 2)
	}
	cat, err := catalog.Scan(cfg, opts.project)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	plan, err := engine.BuildPlan(cfg, cat, opts.agent, opts.project)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 2)
	}
	result, err := engine.Apply(cfg, plan)
	if err != nil {
		return writeError(writer, opts.toon, err, []string{"Run `skilltrim plan` and resolve conflicts."}, 1)
	}
	return writeValue(writer, opts.toon, result)
}

func runRollback(writer io.Writer, opts options, cfg config.File, args []string) int {
	if len(args) != 0 || (opts.agent != "" && opts.agent != "all") || opts.project != "" {
		return writeError(writer, opts.toon, errors.New("rollback restores the complete last apply and does not accept agent or project filters"), nil, 2)
	}
	result, err := engine.Rollback(cfg)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	return writeValue(writer, opts.toon, result)
}

func runDoctor(writer io.Writer, opts options, cfg config.File, args []string) int {
	if len(args) != 0 {
		return writeError(writer, opts.toon, fmt.Errorf("unknown argument %q for `doctor`", args[0]), nil, 2)
	}
	result, err := doctor.Check(cfg, opts.agent, opts.project)
	if err != nil {
		return writeError(writer, opts.toon, err, nil, 1)
	}
	code := writeValue(writer, opts.toon, result)
	if code == 0 && result.Status != "ok" {
		return 1
	}
	return code
}

func runTUI(writer io.Writer, opts options, cfg config.File, args []string) int {
	if len(args) != 0 {
		return writeError(writer, opts.toon, fmt.Errorf("unknown argument %q for `tui`", args[0]), nil, 2)
	}
	if opts.toon {
		return writeError(writer, true, errors.New("--toon is not valid with `tui`"), nil, 2)
	}
	if opts.agent == "all" {
		opts.agent = ""
	}
	if opts.agent != "" {
		if _, ok := cfg.Agents[opts.agent]; !ok {
			return writeError(writer, false, fmt.Errorf("unknown agent %q", opts.agent), nil, 2)
		}
	}
	if err := tui.Run(cfg, opts.configPath, opts.project, opts.agent); err != nil {
		return writeError(writer, false, err, nil, 1)
	}
	return 0
}

func parseOptions(args []string) (options, []string, error) {
	var opts options
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--toon":
			opts.toon = true
		case arg == "--help" || arg == "-h":
			opts.help = true
		case arg == "--agent" || arg == "--project" || arg == "--config":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("%s requires a value", arg)
			}
			i++
			setOption(&opts, arg, args[i])
		case strings.HasPrefix(arg, "--agent="):
			opts.agent = strings.TrimPrefix(arg, "--agent=")
		case strings.HasPrefix(arg, "--project="):
			opts.project = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "--config="):
			opts.configPath = strings.TrimPrefix(arg, "--config=")
		case arg == "--to-project":
			if i+1 >= len(args) {
				return opts, nil, errors.New("--to-project requires a value")
			}
			rest = append(rest, arg, args[i+1])
			i++
		case strings.HasPrefix(arg, "--to-project=") || arg == "--apply":
			rest = append(rest, arg)
		default:
			if strings.HasPrefix(arg, "-") && arg != "--full" && arg != "--force" {
				return opts, nil, fmt.Errorf("unknown flag %q", arg)
			}
			rest = append(rest, arg)
		}
	}
	return opts, rest, nil
}

func setOption(opts *options, name, value string) {
	switch name {
	case "--agent":
		opts.agent = value
	case "--project":
		opts.project = value
	case "--config":
		opts.configPath = value
	}
}

func filterAgents(agents []core.AgentSummary, selected string) ([]core.AgentSummary, error) {
	if selected == "" || selected == "all" {
		return agents, nil
	}
	for _, agent := range agents {
		if agent.Name == selected {
			return []core.AgentSummary{agent}, nil
		}
	}
	return nil, fmt.Errorf("unknown agent %q", selected)
}

func matchPatterns(cat core.Catalog, patterns []string) ([]core.Skill, error) {
	seen := map[string]core.Skill{}
	for _, pattern := range patterns {
		matches, err := catalog.Match(cat, pattern)
		if err != nil {
			return nil, err
		}
		for _, skill := range matches {
			seen[skill.Name] = skill
		}
	}
	names := sortedMapKeys(seen)
	result := make([]core.Skill, 0, len(names))
	for _, name := range names {
		result = append(result, seen[name])
	}
	return result, nil
}

func skillNames(skills []core.Skill) []string {
	names := make([]string, len(skills))
	for i, skill := range skills {
		names[i] = skill.Name
	}
	return names
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func writeValue(writer io.Writer, toon bool, value any) int {
	if err := output.Write(writer, value, toon); err != nil {
		return 1
	}
	return 0
}

func writeError(writer io.Writer, toon bool, err error, help []string, code int) int {
	_ = output.Write(writer, output.Error{Error: err.Error(), Help: help}, toon)
	return code
}

func homeDir() (string, error) {
	if value := os.Getenv("SKILLTRIM_HOME"); value != "" {
		return filepath.Abs(value)
	}
	return os.UserHomeDir()
}

func collapseHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~/" + strings.TrimPrefix(path, home+string(os.PathSeparator))
	}
	return path
}

func commandHelp(command string) string {
	switch command {
	case "list":
		return listHelp
	case "mode":
		return modeHelp
	case "group":
		return groupHelp
	case "move":
		return moveHelp
	case "plan":
		return "usage: skilltrim plan [--agent <agent>] [--project <path>] [--toon]\nPreview exact filesystem changes.\n"
	case "apply":
		return "usage: skilltrim apply [--agent <agent>] [--project <path>] [--toon]\nApply configured changes and save rollback snapshot.\n"
	case "rollback":
		return "usage: skilltrim rollback [--toon]\nRestore complete filesystem state from last apply.\n"
	case "doctor":
		return "usage: skilltrim doctor [--agent <agent>] [--project <path>] [--toon]\nReport broken links, invalid references, and plan conflicts.\n"
	case "tui":
		return "usage: skilltrim tui [--agent <agent>] [--project <path>]\nOpen interactive skill manager.\n"
	default:
		return topHelp
	}
}

const topHelp = `usage: skilltrim [command] [flags]

Control which agent skills enter model context.

commands:
  list       List discovered skills
  mode       Set skill activation mode
  group      Manage router groups
  move       Move global skills into projects
  plan       Preview filesystem changes
  apply      Apply configured changes
  rollback   Restore last applied filesystem state
  doctor     Diagnose configuration and links
  tui        Open interactive manager

global flags:
  --agent <name>    Filter or target codex, claude, cursor, or opencode
  --project <path>  Use project-local skill scope
  --config <path>   Use alternate TOML config
  --toon            Emit TOON instead of JSON
  -h, --help        Show help
  -v, -V, --version Show version
`

const listHelp = `usage: skilltrim list [--full] [--agent <agent>] [--project <path>] [--toon]
List skills from configured libraries and active agent directories.
`

const modeHelp = `usage: skilltrim mode set <skill-or-glob> <mode> --agent <agent> [--project <path>]

modes:
  auto          Directly exposed for automatic matching
  explicit      Available only by direct invocation where supported
  group:<name>  Hidden behind one explicit group router
  off           Not exposed to target agent
`

const groupHelp = `usage: skilltrim group <command>

commands:
  create <name>                    Create empty router group
  add <name> <skill-or-glob>...    Add members; --agent also assigns group mode
  remove <name> <skill-or-glob>... Remove members and turn affected modes off
  list                             List groups and members
  delete <name> [--force]          Delete empty group, or disable members with --force
`

const moveHelp = `usage: skilltrim move <skill-or-glob> --to-project <path> [--to-project <path>...] --agent <name|all> [--apply]

Move globally exposed skills into one or more projects.
Preview by default. Add --apply to update config, links, and local Git exclusions under one rollback snapshot.
`
