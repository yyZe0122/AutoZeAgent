package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// zoneMgr is the package-level bubblezone manager (mouse hit-testing for expand).
var zoneMgr = zone.New()

func zoneMark(id, v string) string {
	if id == "" || zoneMgr == nil {
		return v
	}
	return zoneMgr.Mark(id, v)
}

func zoneScan(v string) string {
	if zoneMgr == nil {
		return v
	}
	return zoneMgr.Scan(v)
}

func zoneHit(msg tea.MouseMsg, id string) bool {
	if zoneMgr == nil || id == "" {
		return false
	}
	z := zoneMgr.Get(id)
	if z == nil {
		return false
	}
	return z.InBounds(msg)
}
