//go:build !linux

package executor

import (
	"context"
	"runtime"
)

type isolationRuntime struct {
	status IsolationStatus
}

func newIsolationRuntime(config IsolationConfig) *isolationRuntime {
	_ = normalizeIsolationConfig(config)
	return &isolationRuntime{
		status: IsolationStatus{
			Mode:   StatusUnsupported,
			Reason: "platform_" + runtime.GOOS,
		},
	}
}

func (rt *isolationRuntime) Status() IsolationStatus {
	if rt == nil {
		return IsolationStatus{Mode: StatusUnsupported, Reason: "isolation unavailable"}
	}
	return rt.status
}

func (rt *isolationRuntime) wrapCommand(context.Context, Request, processPolicy) (string, []string, string, func(), bool) {
	return "", nil, "", func() {}, false
}
