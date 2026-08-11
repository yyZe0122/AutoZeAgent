package providerconfig

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (c ChatConfig) EffectiveRoots(fallback string) []string {
	out := c.configuredRoots()
	if len(out) > 0 {
		return out
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return nil
	}
	return []string{fallback}
}

// WorkspaceAllowAll reports chat.workspace.allow_all.
func (c ChatConfig) WorkspaceAllowAll() bool {
	return c.Workspace != nil && c.Workspace.AllowAll
}

// configuredRoots merges chat.roots and chat.workspace.allow (absolute paths only).
func (c ChatConfig) configuredRoots() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" {
			return
		}
		if _, ok := seen[root]; ok {
			return
		}
		seen[root] = struct{}{}
		out = append(out, root)
	}
	for _, root := range c.Roots {
		add(root)
	}
	if c.Workspace != nil {
		for _, root := range c.Workspace.Allow {
			add(root)
		}
	}
	return out
}

// PathCeilingRoots returns roots for PathGuard at daemon start (no client cwd).
// When allow_all, returns nil (caller uses unrestricted guard).
// Otherwise: configured roots, else daemonFallback.
func (c ChatConfig) PathCeilingRoots(daemonFallback string) []string {
	if c.WorkspaceAllowAll() {
		return nil
	}
	out := c.configuredRoots()
	if len(out) > 0 {
		return out
	}
	daemonFallback = strings.TrimSpace(daemonFallback)
	if daemonFallback == "" {
		return nil
	}
	return []string{daemonFallback}
}

// ResolveSessionWorkspace picks the workspace root for a session/task (ADR-046).
// clientWorkspace is the absolute path from the client (usually Getwd).
// daemonFallback is the daemon process cwd.
func (c ChatConfig) ResolveSessionWorkspace(clientWorkspace, daemonFallback string) string {
	clientWorkspace = strings.TrimSpace(clientWorkspace)
	daemonFallback = strings.TrimSpace(daemonFallback)
	if c.Workspace != nil {
		def := strings.TrimSpace(c.Workspace.Default)
		switch {
		case def == "" || strings.EqualFold(def, WorkspaceDefaultClientCWD):
			if clientWorkspace != "" {
				return clientWorkspace
			}
			if daemonFallback != "" {
				return daemonFallback
			}
		case strings.EqualFold(def, WorkspaceDefaultDaemonCWD):
			if daemonFallback != "" {
				return daemonFallback
			}
			return clientWorkspace
		case filepath.IsAbs(def):
			return def
		}
	}
	// Legacy: fixed roots only → first root; else client then daemon.
	if roots := c.configuredRoots(); len(roots) == 1 && clientWorkspace == "" {
		return roots[0]
	}
	if clientWorkspace != "" {
		return clientWorkspace
	}
	if roots := c.configuredRoots(); len(roots) > 0 {
		return roots[0]
	}
	return daemonFallback
}

// GrantRootsForSession returns plan/grant path list: session workspace + configured extras.
func (c ChatConfig) GrantRootsForSession(sessionWorkspace string) []string {
	if c.WorkspaceAllowAll() {
		sessionWorkspace = strings.TrimSpace(sessionWorkspace)
		if sessionWorkspace == "" {
			return nil
		}
		return []string{sessionWorkspace}
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" {
			return
		}
		if _, ok := seen[root]; ok {
			return
		}
		seen[root] = struct{}{}
		out = append(out, root)
	}
	add(sessionWorkspace)
	for _, root := range c.configuredRoots() {
		add(root)
	}
	return out
}

// AcceptsWorkspace reports whether client workspace is allowed under ceiling policy.
func (c ChatConfig) AcceptsWorkspace(workspace string) bool {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return false
	}
	if c.WorkspaceAllowAll() {
		return true
	}
	// client_cwd default: any absolute client path is accepted (session-bound).
	def := WorkspaceDefaultClientCWD
	if c.Workspace != nil && strings.TrimSpace(c.Workspace.Default) != "" {
		def = strings.TrimSpace(c.Workspace.Default)
	}
	if strings.EqualFold(def, WorkspaceDefaultClientCWD) || def == "" {
		return true
	}
	if strings.EqualFold(def, WorkspaceDefaultDaemonCWD) {
		return true // resolved to daemon path by ResolveSessionWorkspace
	}
	if filepath.IsAbs(def) {
		return workspace == def || pathUnder(def, workspace)
	}
	// Must be under configured roots.
	for _, root := range c.configuredRoots() {
		if workspace == root || pathUnder(root, workspace) {
			return true
		}
	}
	return false
}

func pathUnder(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == path {
		return true
	}
	prefix := root + string(filepath.Separator)
	return strings.HasPrefix(path, prefix)
}

func (c ChatConfig) validate() error {
	for i, root := range c.Roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			return fmt.Errorf("chat.roots[%d] must be an absolute path", i)
		}
	}
	if c.Workspace != nil {
		def := strings.TrimSpace(c.Workspace.Default)
		if def != "" && !strings.EqualFold(def, WorkspaceDefaultClientCWD) &&
			!strings.EqualFold(def, WorkspaceDefaultDaemonCWD) && !filepath.IsAbs(def) {
			return fmt.Errorf("chat.workspace.default must be client_cwd, daemon_cwd, or an absolute path")
		}
		for i, root := range c.Workspace.Allow {
			root = strings.TrimSpace(root)
			if root == "" {
				continue
			}
			if !filepath.IsAbs(root) {
				return fmt.Errorf("chat.workspace.allow[%d] must be an absolute path", i)
			}
		}
	}
	if c.MaxIterations != 0 && (c.MaxIterations < 1 || c.MaxIterations > 64) {
		return fmt.Errorf("chat.max_iterations must be between 1 and 64 (or omit for default 16)")
	}
	if c.Permission != nil {
		mode := strings.ToLower(strings.TrimSpace(c.Permission.Mode))
		if mode != "" && mode != PermissionModePreauth && mode != PermissionModeAsk && mode != PermissionModeAuto {
			return fmt.Errorf("chat.permission.mode must be preauth, ask, or auto")
		}
	}
	if c.Memory != nil {
		if c.Memory.MaxInjectRunes != 0 && (c.Memory.MaxInjectRunes < 200 || c.Memory.MaxInjectRunes > 16_000) {
			return fmt.Errorf("chat.memory.max_inject_runes must be between 200 and 16000 (or omit)")
		}
		mode := strings.ToLower(strings.TrimSpace(c.Memory.InjectMode))
		if mode != "" && mode != "session_start" {
			return fmt.Errorf("chat.memory.inject_mode must be session_start (or omit)")
		}
	}
	return nil
}

// LoadModelRoles returns the main model ref and optional role overrides from config.
// roles keys are only configured non-empty overrides (subagent, compact).
// Unset or empty values fall back to main at call sites.
