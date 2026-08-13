package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yyZe0122/yunmengze-agent/internal/gatewayclient"
	"github.com/yyZe0122/yunmengze-agent/internal/modelstream"
	"github.com/yyZe0122/yunmengze-agent/pkg/eventapi"
	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
)

func (m model) applyModelStream(env modelstream.Envelope) (tea.Model, tea.Cmd) {
	// Ignore streams for other sessions when focused.
	if m.sessionID != "" && env.SessionID != "" && gatewayclient.SessionID(env.SessionID) != m.sessionID {
		return m, nil
	}
	if env.RunID != "" {
		m.liveRunID = gatewayclient.RunID(env.RunID)
	}
	switch env.Event.Type {
	case providerapi.StreamDelta:
		m.liveContent += env.Event.ContentDelta
	case providerapi.StreamThinking:
		m.liveThinking += env.Event.ThinkingDelta
	case providerapi.StreamToolCall:
		if env.Event.ToolCall != nil {
			preview := toolCallPreview(env.Event.ToolCall.Name, env.Event.ToolCall.Arguments)
			m.liveTools = append(m.liveTools, contentBlock{
				Kind:     blockToolCall,
				Text:     preview,
				ToolName: env.Event.ToolCall.Name,
				ToolID:   env.Event.ToolCall.ID,
				Key:      "live-tc:" + env.Event.ToolCall.ID,
				Live:     true,
			})
		}
	case providerapi.StreamComplete:
		m.liveContent = ""
		m.liveThinking = ""
		m.liveTools = nil
		m.liveRunID = ""
		return m, m.scheduleRefresh(refreshFull)
	default:
		if env.Event.ContentDelta != "" {
			m.liveContent += env.Event.ContentDelta
		}
		if env.Event.ThinkingDelta != "" {
			m.liveThinking += env.Event.ThinkingDelta
		}
	}
	m.timeline = buildChatTimeline(m.messages, m.task, m.plan, m.runs)
	m.timeline = appendLiveDraft(m.timeline, m.liveThinking, m.liveContent, m.liveTools)
	m.syncViewport(true)
	return m, nil
}

func (m model) applyPermPoll(msg permPollDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, nil
	}
	prev := m.pendingPermCount
	m.permissions = msg.permissions
	m.pendingPermCount = len(msg.permissions)
	if m.pendingPermCount == 0 {
		m.autoOpenedPermList = false
		if m.list == listPermissions {
			m.closeList()
		}
	} else if msg.openList && prev == 0 && m.list == listNone {
		m.openPermissionsWithGrace()
		m.statusMsg = fmt.Sprintf("%d tool permission(s) pending · 1 once · 2 similar · 3 permanent · 4 deny", m.pendingPermCount)
	} else if m.list == listPermissions {
		// Keep picker in sync with latest rows.
		if m.selectedIdx >= len(m.permissions) && len(m.permissions) > 0 {
			m.selectedIdx = len(m.permissions) - 1
		}
	}
	if m.task != nil && m.task.State == gatewayclient.TaskStateRunning {
		m.timeline = patchRunningStatus(m.timeline, runningStatusTitle(m.task, m.pendingPermCount))
		m.syncViewport(false)
	}
	m.layout()
	return m, nil
}

func (m *model) shouldPollPermissions() bool {
	if m.needsRunPoll() {
		return true
	}
	return m.pendingPermCount > 0
}

func (m model) applySSE(envelope eventapi.Envelope) (tea.Model, tea.Cmd) {
	if envelope.Sequence > m.sseAfter {
		m.sseAfter = envelope.Sequence
	}
	typ := envelope.Type
	// C1: permission.pending / permission.decided → refresh perm queue (still DecidePermission*).
	if strings.HasPrefix(typ, "permission.") {
		autoOpen := !m.autoOpenedPermList && m.list == listNone && !m.completer.visible
		return m, m.pollPermissionsCmd(autoOpen)
	}
	var kind refreshKind
	switch {
	case strings.HasPrefix(typ, "run."):
		kind = refreshRuns
	case strings.HasPrefix(typ, "plan."), strings.HasPrefix(typ, "approval."):
		kind = refreshPlan
	case strings.HasPrefix(typ, "task."):
		kind = refreshTask
	default:
		return m, nil
	}
	// Prefer immediate coalesced refresh over waiting for tick.
	return m, m.scheduleRefresh(kind)
}
