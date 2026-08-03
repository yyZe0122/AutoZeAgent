package contextpack

import (
	"strings"
	"sync"
)

const (
	ratioMin      = 0.5
	ratioMax      = 2.5
	ratioEMAAlpha = 0.3
)

// Calibrator keeps per-model estimate→actual ratios (EMA).
type Calibrator struct {
	mu     sync.RWMutex
	ratios map[string]float64
}

// NewCalibrator returns an empty calibrator.
func NewCalibrator() *Calibrator {
	return &Calibrator{ratios: make(map[string]float64)}
}

// Ratio returns the calibration factor for model (1.0 if unknown).
func (c *Calibrator) Ratio(model string) float64 {
	if c == nil {
		return 1
	}
	key := strings.TrimSpace(model)
	c.mu.RLock()
	defer c.mu.RUnlock()
	if r, ok := c.ratios[key]; ok && r > 0 {
		return r
	}
	return 1
}

// Observe updates the ratio from raw estimate and actual prompt tokens.
func (c *Calibrator) Observe(model string, estimate, actual int64) {
	if c == nil || estimate <= 0 || actual <= 0 {
		return
	}
	key := strings.TrimSpace(model)
	if key == "" {
		return
	}
	sample := float64(actual) / float64(estimate)
	if sample < ratioMin {
		sample = ratioMin
	}
	if sample > ratioMax {
		sample = ratioMax
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ratios == nil {
		c.ratios = make(map[string]float64)
	}
	if prev, ok := c.ratios[key]; ok && prev > 0 {
		c.ratios[key] = prev*(1-ratioEMAAlpha) + sample*ratioEMAAlpha
	} else {
		c.ratios[key] = sample
	}
}

// Apply multiplies raw estimate by the model ratio.
func (c *Calibrator) Apply(model string, raw int64) int64 {
	if raw <= 0 {
		return 0
	}
	r := c.Ratio(model)
	out := int64(float64(raw)*r + 0.5)
	if out < 1 {
		return 1
	}
	return out
}
