package tui

import (
	"strings"
)

type slashCommand struct {
	Name string
	Desc string
	Help string
}

var slashCommands = []slashCommand{
	{Name: "/new", Desc: "new chat session", Help: "/new [message…]  start a fresh session"},
	{Name: "/sessions", Desc: "list chat sessions", Help: "/sessions  list sessions (open with Enter)"},
	{Name: "/tasks", Desc: "list / focus tasks", Help: "/tasks [id-prefix]  list tasks or focus by id"},
	{Name: "/back", Desc: "session list", Help: "/back  open session list"},
	{Name: "/clear", Desc: "session list", Help: "/clear  open session list"},
	{Name: "/pause", Desc: "pause task", Help: "/pause [reason]  pause current task"},
	{Name: "/resume", Desc: "resume task", Help: "/resume  resume current task"},
	{Name: "/cancel", Desc: "cancel task", Help: "/cancel [reason]  cancel current task (/stop)"},
	{Name: "/stop", Desc: "cancel task", Help: "/stop [reason]  alias for /cancel"},
	{Name: "/retry", Desc: "resubmit last user message", Help: "/retry  resubmit last user message on focused session"},
	{Name: "/model", Desc: "list or switch model", Help: "/model [provider/model]  pick or switch model"},
	{Name: "/skills", Desc: "select skills for next submit", Help: "/skills  toggle skills for next task (explicit only)"},
	{Name: "/theme", Desc: "toggle day/night theme", Help: "/theme  toggle day ↔ night theme"},
	{Name: "/cron", Desc: "list or create scheduled jobs", Help: "/cron [every objective]  list jobs, or create on current session (Tab mode)"},
	{Name: "/compact", Desc: "compact session context", Help: "/compact [focus]  force session head summary (optional focus text)"},
	{Name: "/perm", Desc: "tool permission queue", Help: "/perm open; keys 1–4 once|similar|permanent|deny; /perm <decision> <id>"},
	{Name: "/memory", Desc: "list/search local memory", Help: "/memory [q…]  list facts; /memory forget|promote <id>; /memory refresh"},
	{Name: "/refresh-memory", Desc: "rebuild frozen memory inject", Help: "/refresh-memory  invalidate session memory snapshot (next turn reinjects)"},
	{Name: "/status", Desc: "health summary", Help: "/status  health + model + task + context + pending perms"},
	{Name: "/help", Desc: "command list", Help: "/help  command list"},
	{Name: "/quit", Desc: "exit", Help: "/quit  exit TUI (/q /exit)"},
}

// canonicalSlash maps aliases to primary command names.
func canonicalSlash(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "/q", "/exit":
		return "/quit"
	case "/clear", "/resume-list":
		return "/back"
	case "/sessions":
		return "/sessions"
	case "/stop":
		return "/cancel"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func helpText() string {
	var b strings.Builder
	b.WriteString("Commands\n")
	for _, cmd := range slashCommands {
		b.WriteString("  ")
		b.WriteString(cmd.Help)
		b.WriteByte('\n')
	}
	b.WriteString("\nKeys\n")
	b.WriteString("  Tab            complete slash, else toggle agent↔plan mode\n")
	b.WriteString("  Shift+Tab      toggle agent↔plan mode\n")
	b.WriteString("  ↑↓             picker / completer / history (not chat)\n")
	b.WriteString("  PgUp/PgDn      always scroll conversation\n")
	b.WriteString("  Enter          complete slash once, then execute; open picker item\n")
	b.WriteString("  Esc            close picker / completer / clear input\n")
	b.WriteString("  Ctrl+C         clear input (exit via /quit only)\n")
	b.WriteString("\nModes (input border + chip)\n")
	b.WriteString("  agent  multi-turn chat + workspace tools (build/write)\n")
	b.WriteString("  plan   multi-turn chat + workspace tools (read-only)\n")
	b.WriteString("\nChat\n")
	b.WriteString("  Plain text continues the current session (or starts one).\n")
	b.WriteString("  /new always opens a fresh session. Tab sets agent|plan mode.\n")
	b.WriteString("  /skills selects instruction skills for the next submit (not auto-matched).\n")
	b.WriteString("  /perm opens pending tool permissions when chat.permission.mode=ask.\n")
	b.WriteString("  /retry resubmits the last user message on the focused session.\n")
	return b.String()
}

func parseSlash(line string) (name, arg string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ""
	}
	if !strings.HasPrefix(line, "/") {
		return "", line
	}
	parts := strings.SplitN(line, " ", 2)
	name = canonicalSlash(parts[0])
	if len(parts) == 2 {
		arg = strings.TrimSpace(parts[1])
	}
	return name, arg
}
