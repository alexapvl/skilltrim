package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alexapvl/skilltrim/internal/catalog"
	"github.com/alexapvl/skilltrim/internal/config"
	"github.com/alexapvl/skilltrim/internal/core"
	"github.com/alexapvl/skilltrim/internal/engine"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenSkills screen = iota
	screenPlan
	screenConfirm
	screenDiscard
)

type model struct {
	cfg        config.File
	configPath string
	project    string
	catalog    core.Catalog
	agents     []string
	agentIndex int
	cursor     int
	width      int
	height     int
	searching  bool
	search     textinput.Model
	screen     screen
	plan       engine.Plan
	status     string
	dirty      bool
}

var (
	accentStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "24", Dark: "81"}).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "242", Dark: "245"})
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "203"}).Bold(true)
)

func Run(cfg config.File, configPath, project, initialAgent string) error {
	cat, err := catalog.Scan(cfg, project)
	if err != nil {
		return err
	}
	m := newModel(cfg, configPath, project, initialAgent, cat)
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func newModel(cfg config.File, configPath, project, initialAgent string, cat core.Catalog) model {
	agents := cfg.AgentNames()
	index := 0
	for i, agent := range agents {
		if agent == initialAgent {
			index = i
			break
		}
	}
	search := textinput.New()
	search.Placeholder = "filter skills"
	search.Prompt = "/ "
	search.CharLimit = 80
	return model{cfg: cfg, configPath: configPath, project: project, catalog: cat, agents: agents, agentIndex: index, search: search}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		switch m.screen {
		case screenPlan:
			return m.updatePlan(msg)
		case screenConfirm:
			return m.updateConfirm(msg)
		case screenDiscard:
			return m.updateDiscard(msg)
		default:
			return m.updateSkills(msg)
		}
	}
	return m, nil
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searching = false
		m.search.Blur()
		m.cursor = 0
		return m, nil
	case "esc":
		m.searching = false
		m.search.Blur()
		m.search.SetValue("")
		m.cursor = 0
		return m, nil
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.cursor = 0
	return m, cmd
}

func (m model) updateSkills(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.filteredSkills()
	switch msg.String() {
	case "q":
		return m.requestQuit()
	case "/":
		m.searching = true
		return m, m.search.Focus()
	case "x":
		m.search.SetValue("")
		m.cursor = 0
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor+1 < len(items) {
			m.cursor++
		}
	case "tab":
		if len(m.agents) > 0 {
			m.agentIndex = (m.agentIndex + 1) % len(m.agents)
			m.cursor = 0
		}
	case "shift+tab":
		if len(m.agents) > 0 {
			m.agentIndex = (m.agentIndex - 1 + len(m.agents)) % len(m.agents)
			m.cursor = 0
		}
	case " ":
		if len(items) > 0 {
			m.cycleMode(items[m.cursor])
		}
	case "g":
		if len(items) > 0 {
			m.cycleGroup(items[m.cursor])
		}
	case "p":
		m.buildPlan()
		m.screen = screenPlan
	case "a":
		m.buildPlan()
		if len(m.plan.Conflicts) > 0 {
			m.status = fmt.Sprintf("Resolve %d conflict(s) before apply", len(m.plan.Conflicts))
			m.screen = screenPlan
		} else {
			m.screen = screenConfirm
		}
	case "r":
		if m.dirty {
			m.status = "Staged changes present. Apply them or quit and discard first."
		} else {
			m.reload()
		}
	}
	return m, nil
}

func (m model) updatePlan(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.requestQuit()
	case "esc", "p":
		m.screen = screenSkills
	case "a":
		if len(m.plan.Conflicts) == 0 {
			m.screen = screenConfirm
		}
	}
	return m, nil
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		m.apply()
		m.screen = screenSkills
	case "n", "esc":
		m.screen = screenSkills
	case "q":
		return m.requestQuit()
	}
	return m, nil
}

func (m model) updateDiscard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter", "q":
		return m, tea.Quit
	case "n", "esc":
		m.screen = screenSkills
	}
	return m, nil
}

func (m model) requestQuit() (tea.Model, tea.Cmd) {
	if m.dirty {
		m.screen = screenDiscard
		return m, nil
	}
	return m, tea.Quit
}

func (m model) View() string {
	if len(m.agents) == 0 {
		return "SkillTrim\n\nNo agent adapters configured.\n\nq quit"
	}
	switch m.screen {
	case screenPlan:
		return m.planView()
	case screenConfirm:
		return m.confirmView()
	case screenDiscard:
		return m.discardView()
	default:
		return m.skillsView()
	}
}

func (m model) skillsView() string {
	agent := m.currentAgent()
	summary := m.agentSummary(agent)
	scope := core.Scope(m.project)
	if scope != "global" {
		scope = filepath.Base(scope)
	}
	header := fmt.Sprintf("%s  %s  %s", accentStyle.Render("SkillTrim"), agent, dimStyle.Render(scope))
	stats := fmt.Sprintf("%d active  %d context chars  ~%d tokens", summary.Active, summary.ContextChars, summary.EstimatedTokens)
	if summary.Explicit > 0 {
		stats += fmt.Sprintf("  %d explicit", summary.Explicit)
	}
	if m.dirty {
		stats += "  staged changes"
	}
	var body strings.Builder
	body.WriteString(header + "\n" + stats + "\n\n")
	if m.searching || m.search.Value() != "" {
		body.WriteString(m.search.View() + "\n\n")
	}
	items := m.filteredSkills()
	if len(items) == 0 {
		body.WriteString("No matching skills. Press / to change filter.\n")
	} else {
		limit := m.height - 8
		if limit < 5 {
			limit = 10
		}
		start := 0
		if m.cursor >= limit {
			start = m.cursor - limit + 1
		}
		end := min(len(items), start+limit)
		for i := start; i < end; i++ {
			skill := items[i]
			mode := m.mode(skill)
			line := fmt.Sprintf("  %-30s %-16s %6d", truncate(skill.Name, 30), mode, skill.ContextChars)
			if m.width > 0 && m.width < 64 {
				nameWidth := max(12, m.width-22)
				line = fmt.Sprintf("  %-*s %s", nameWidth, truncate(skill.Name, nameWidth), mode)
			}
			if i == m.cursor {
				line = selectedStyle.Render(">" + line[1:])
			}
			body.WriteString(line + "\n")
		}
	}
	body.WriteString("\n" + dimStyle.Render("↑/↓ move  space mode  g group  / search  p plan  a apply  tab agent  r reload  q quit"))
	if m.status != "" {
		body.WriteString("\n" + m.status)
	}
	return body.String()
}

func (m model) planView() string {
	var body strings.Builder
	body.WriteString(accentStyle.Render("Change preview") + "\n\n")
	for _, change := range m.plan.Context {
		line := fmt.Sprintf("%-13s %-8s %d → %d chars (%+d)", "context", change.Agent, change.BeforeChars, change.AfterChars, -change.SavedChars)
		body.WriteString(truncate(line, max(20, m.width)) + "\n")
	}
	if len(m.plan.Context) > 0 {
		body.WriteString("\n")
	}
	if len(m.plan.Operations) == 0 && len(m.plan.Conflicts) == 0 {
		body.WriteString("No filesystem changes.\n")
	}
	for _, op := range m.plan.Operations {
		line := fmt.Sprintf("%-13s %-8s %s", op.Action, op.Agent, truncateHome(op.Path))
		body.WriteString(truncate(line, max(20, m.width)) + "\n")
	}
	for _, conflict := range m.plan.Conflicts {
		line := fmt.Sprintf("conflict      %-8s %s: %s", conflict.Agent, truncateHome(conflict.Path), conflict.Message)
		body.WriteString(errorStyle.Render(truncate(line, max(20, m.width))) + "\n")
	}
	body.WriteString("\n" + dimStyle.Render("a apply  esc back  q quit"))
	return body.String()
}

func (m model) confirmView() string {
	return accentStyle.Render("Apply changes?") + fmt.Sprintf("\n\n%d filesystem operation(s). Active links get a rollback snapshot.\n\n", len(m.plan.Operations)) + dimStyle.Render("y/enter apply  n/esc cancel")
}

func (m model) discardView() string {
	return accentStyle.Render("Discard staged changes?") + "\n\nNo filesystem changes were applied.\n\n" + dimStyle.Render("y/enter discard and quit  n/esc keep editing")
}

func (m *model) cycleMode(skill core.Skill) {
	current := m.mode(skill)
	next := core.ModeAuto
	switch current {
	case core.ModeAuto:
		if m.currentAgent() == "codex" || m.currentAgent() == "claude" {
			next = core.ModeExplicit
		} else {
			next = core.ModeOff
		}
	case core.ModeExplicit:
		next = core.ModeOff
	case core.ModeOff:
		next = core.ModeAuto
	default:
		next = core.ModeAuto
	}
	m.cfg.SetMode(skill.Name, m.currentAgent(), core.Scope(m.project), next)
	m.cfg.AddSource(filepath.Dir(skill.Source))
	m.dirty = true
	m.status = fmt.Sprintf("%s → %s", skill.Name, next)
}

func (m *model) cycleGroup(skill core.Skill) {
	if len(m.cfg.Groups) == 0 {
		m.status = "No groups configured. Use `skilltrim group create <name>`."
		return
	}
	groups := append([]core.Group(nil), m.cfg.Groups...)
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	current := m.mode(skill)
	index := -1
	if current.IsGroup() {
		for i, group := range groups {
			if group.Name == current.Group() {
				index = i
				break
			}
		}
	}
	next := core.Mode("group:" + groups[(index+1)%len(groups)].Name)
	m.cfg.SetMode(skill.Name, m.currentAgent(), core.Scope(m.project), next)
	m.cfg.AddSource(filepath.Dir(skill.Source))
	m.dirty = true
	m.status = fmt.Sprintf("%s → %s", skill.Name, next)
}

func (m *model) buildPlan() {
	plan, err := engine.BuildPlan(m.cfg, m.catalog, "", m.project)
	if err != nil {
		m.status = err.Error()
		m.plan = engine.Plan{}
		return
	}
	m.plan = plan
}

func (m *model) apply() {
	old, readErr := os.ReadFile(m.configPath)
	existed := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		m.status = readErr.Error()
		return
	}
	if err := config.Save(m.configPath, m.cfg); err != nil {
		m.status = err.Error()
		return
	}
	result, err := engine.Apply(m.cfg, m.plan)
	if err != nil {
		if existed {
			_ = os.WriteFile(m.configPath, old, 0o644)
		} else {
			_ = os.Remove(m.configPath)
		}
		m.status = err.Error()
		return
	}
	m.dirty = false
	if result.Noop {
		m.status = "Already applied."
	} else {
		m.status = fmt.Sprintf("Applied %d operation(s).", result.Applied)
	}
	m.reloadCatalog()
}

func (m *model) reload() {
	cfg, err := config.Load(m.configPath, userHome())
	if err != nil {
		m.status = err.Error()
		return
	}
	m.cfg = cfg
	m.dirty = false
	m.reloadCatalog()
	if m.status == "" {
		m.status = "Reloaded."
	}
}

func (m *model) reloadCatalog() {
	cat, err := catalog.Scan(m.cfg, m.project)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.catalog = cat
	items := m.filteredSkills()
	if m.cursor >= len(items) {
		m.cursor = max(0, len(items)-1)
	}
}

func (m model) filteredSkills() []core.Skill {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	if query == "" {
		return m.catalog.Skills
	}
	result := make([]core.Skill, 0)
	for _, skill := range m.catalog.Skills {
		if strings.Contains(strings.ToLower(skill.Name), query) || strings.Contains(strings.ToLower(skill.Description), query) {
			result = append(result, skill)
		}
	}
	return result
}

func (m model) currentAgent() string {
	if len(m.agents) == 0 {
		return ""
	}
	return m.agents[m.agentIndex]
}

func (m model) mode(skill core.Skill) core.Mode {
	if mode, configured := m.cfg.RuleMode(skill.Name, m.currentAgent(), core.Scope(m.project)); configured {
		return mode
	}
	if contains(skill.ImplicitAgents, m.currentAgent()) {
		return core.ModeAuto
	}
	if contains(skill.ActiveAgents, m.currentAgent()) {
		return core.ModeExplicit
	}
	return core.ModeOff
}

func (m model) agentSummary(name string) core.AgentSummary {
	for _, summary := range m.catalog.Agents {
		if summary.Name == name {
			return summary
		}
	}
	return core.AgentSummary{Name: name}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func truncate(value string, width int) string {
	if len(value) <= width {
		return value
	}
	if width <= 1 {
		return value[:width]
	}
	return value[:width-1] + "…"
}

func truncateHome(path string) string {
	home := userHome()
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~/" + strings.TrimPrefix(path, home+string(os.PathSeparator))
	}
	return path
}

func userHome() string {
	home, _ := os.UserHomeDir()
	return home
}
