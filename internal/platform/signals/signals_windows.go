//go:build windows

package signals

import "os"

func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
