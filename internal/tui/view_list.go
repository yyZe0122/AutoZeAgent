package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"autozeagent.local/autozeagent/internal/gatewayclient"
	"autozeagent.local/autozeagent/pkg/schedulerapi"
)

func (m *model) inListMode() bool {
	return m.list != listNone
}

func (m *model) listLen() int {
	switch m.list {
	case listModels:
		return len(m.models)
	case listJobs:
		return len(m.jobs)
	case listSessions:
		return len(m.sessions)
	case listTasks:
		return len(m.tasks)
	case listSkills:
		return len(m.skills)
	default:
		return 0
	}
}

func (m *model) listTitle() string {
	switch m.list {
	case listModels:
		return "Models"
	case listJobs:
		return "Scheduled jobs"
	case listSessions:
		return "Sessions"
	case listTasks:
		return "Tasks"
	case listSkills:
		n := len(m.selectedSkillIDs)
		if n == 0 {
			return "Skills"
		}
		return fmt.Sprintf("Skills (%d selected)", n)
	default:
		return "Picker"
	}
}

func (m *model) listHint() string {
	switch m.list {
	case listModels:
		return "↑↓ select · Enter switch · Esc close · PgUp scroll chat"
	case listJobs:
		return "↑↓ · Enter details · Esc · /cron <every> <obj> create"
	case listSessions:
		return "↑↓ select · Enter open chat · Esc close · PgUp scroll"
	case listTasks:
		return "↑↓ select · Enter focus task · Esc close"
	case listSkills:
		return "↑↓ · Enter toggle · Esc done (applies to next submit)"
	default:
		return "↑↓ · Enter · Esc"
	}
}

func (m *model) listLine(i int) string {
	switch m.list {
	case listModels:
		if i < 0 || i >= len(m.models) {
			return ""
		}
		name := m.models[i]
		mark := "  "
		if name == m.modelName {
			mark = "* "
		}
		return mark + name
	case listJobs:
		if i < 0 || i >= len(m.jobs) {
			return ""
		}
		return formatJobLine(m.jobs[i])
	case listSessions:
		if i < 0 || i >= len(m.sessions) {
			return ""
		}
		s := m.sessions[i]
		title := s.Title
		if title == "" {
			title = string(s.ID)
		}
		mark := "  "
		if s.ID == m.sessionID {
			mark = "* "
		}
		return fmt.Sprintf("%s%s  %s  %s",
			mark,
			shortID(string(s.ID)),
			stateBadge(s.LatestState),
			styleDim.Render(truncate(title, 48)),
		)
	case listSkills:
		if i < 0 || i >= len(m.skills) {
			return ""
		}
		sk := m.skills[i]
		mark := "  "
		if skillSelected(m.selectedSkillIDs, sk.ID) {
			mark = "* "
		}
		label := sk.Name
		if label == "" {
			label = sk.ID
		}
		src := sk.Source
		if src == "" {
			src = "?"
		}
		return fmt.Sprintf("%s%s  [%s]  %s",
			mark,
			sk.ID,
			src,
			styleDim.Render(truncate(label, 40)),
		)
	default:
		if i < 0 || i >= len(m.tasks) {
			return ""
		}
		task := m.tasks[i]
		return fmt.Sprintf("%s  %s  %s",
			shortID(string(task.ID)),
			stateBadge(task.State),
			styleDim.Render(truncate(task.Title, 48)),
		)
	}
}

func skillSelected(ids []string, id string) bool {
	for _, s := range ids {
		if s == id {
			return true
		}
	}
	return false
}

func toggleSkillID(ids []string, id string) []string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ids
	}
	for i, s := range ids {
		if s == id {
			return append(ids[:i], ids[i+1:]...)
		}
	}
	return append(ids, id)
}

func formatJobLine(job schedulerapi.Job) string {
	name := job.Name
	if name == "" {
		name = "(unnamed)"
	}
	next := job.NextRunAt
	if next == "" {
		next = "—"
	}
	mode := job.ExecutionMode
	if mode == "" {
		mode = "agent"
	}
	return fmt.Sprintf("%s  %s  %s  %s  next %s",
		shortID(job.ID),
		stateBadge(job.Status),
		mode,
		truncate(name, 24),
		styleDim.Render(truncate(next, 24)),
	)
}

// renderPickerOverlay draws the floating list above the input (not in viewport).
func renderPickerOverlay(m *model, width int) string {
	if m.list == listNone {
		return ""
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render(m.listTitle()) + "  ")
	b.WriteString(styleDim.Render(m.listHint()) + "\n")
	n := m.listLen()
	if n == 0 {
		empty := "No items."
		switch m.list {
		case listModels:
			empty = "No models. Use /model provider/model."
		case listJobs:
			empty = "No jobs. /cron 15m <objective> on a focused session."
		case listSessions:
			empty = "No sessions yet. Type a message to start."
		case listSkills:
			empty = "No skills found. Add <id>/SKILL.md under config or .autozeagent/skills."
		default:
			empty = "No tasks yet."
		}
		b.WriteString(styleDim.Render(empty))
	} else {
		maxLines := overlayMaxLines
		start := 0
		if m.selectedIdx >= maxLines {
			start = m.selectedIdx - maxLines + 1
		}
		end := start + maxLines
		if end > n {
			end = n
		}
		for i := start; i < end; i++ {
			marker := "  "
			if i == m.selectedIdx {
				marker = styleCompSel.Render("› ")
			}
			line := m.listLine(i)
			// single-line for overlay compactness
			if idx := strings.IndexByte(line, '\n'); idx >= 0 {
				line = line[:idx]
			}
			b.WriteString(marker + line)
			if i < end-1 {
				b.WriteByte('\n')
			}
		}
	}
	boxW := max(40, width-2)
	return stylePickerBox.Width(boxW).Render(b.String())
}

func (m *model) openList(kind listKind) {
	m.list = kind
	m.selectedIdx = 0
	m.helpOpen = false
	switch kind {
	case listModels:
		for i, name := range m.models {
			if name == m.modelName {
				m.selectedIdx = i
				break
			}
		}
	case listSessions:
		// Keep current task/timeline mounted; picker floats above.
	case listSkills:
		// Multi-select; selection lives in selectedSkillIDs.
	}
}

func (m *model) closeList() {
	m.list = listNone
	m.selectedIdx = 0
}

func (m *model) listEnter() tea.Cmd {
	switch m.list {
	case listModels:
		if m.selectedIdx < 0 || m.selectedIdx >= len(m.models) {
			return nil
		}
		name := m.models[m.selectedIdx]
		m.busy = true
		return m.modelCommandCmd(name)
	case listJobs:
		if m.selectedIdx < 0 || m.selectedIdx >= len(m.jobs) {
			return nil
		}
		job := m.jobs[m.selectedIdx]
		m.statusMsg = formatJobDetail(job)
		m.errMsg = ""
		return nil
	case listSessions:
		if m.selectedIdx < 0 || m.selectedIdx >= len(m.sessions) {
			return nil
		}
		s := m.sessions[m.selectedIdx]
		m.sessionID = s.ID
		if s.LatestTaskID != nil {
			m.task = &gatewayclient.Task{ID: *s.LatestTaskID}
		} else {
			m.task = nil
		}
		m.closeList()
		m.planID = ""
		m.plan = nil
		m.runs = nil
		m.messages = nil
		m.viewportContent = ""
		m.stickBottom = true
		return m.scheduleRefresh(refreshFull)
	case listTasks:
		if m.selectedIdx < 0 || m.selectedIdx >= len(m.tasks) {
			return nil
		}
		t := m.tasks[m.selectedIdx]
		m.task = &t
		if t.SessionID != nil {
			m.sessionID = *t.SessionID
		}
		m.closeList()
		m.planID = ""
		m.plan = nil
		m.runs = nil
		m.messages = nil
		m.viewportContent = ""
		m.stickBottom = true
		return m.scheduleRefresh(refreshFull)
	case listSkills:
		if m.selectedIdx < 0 || m.selectedIdx >= len(m.skills) {
			return nil
		}
		sk := m.skills[m.selectedIdx]
		m.selectedSkillIDs = toggleSkillID(m.selectedSkillIDs, sk.ID)
		n := len(m.selectedSkillIDs)
		if n == 0 {
			m.statusMsg = "no skills selected for next submit"
		} else {
			m.statusMsg = fmt.Sprintf("%d skill(s) for next submit: %s", n, strings.Join(m.selectedSkillIDs, ", "))
		}
		m.errMsg = ""
		return nil
	default:
		return nil
	}
}

func formatJobDetail(job schedulerapi.Job) string {
	name := job.Name
	if name == "" {
		name = "(unnamed)"
	}
	mode := job.ExecutionMode
	if mode == "" {
		mode = "agent"
	}
	return fmt.Sprintf("job %s  %s  status=%s  mode=%s  interval=%ds  next=%s",
		shortID(job.ID), name, job.Status, mode, job.IntervalSeconds, orDash(job.NextRunAt))
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
