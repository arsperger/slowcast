package tfrc

import "fmt"

type smoothedRTTHistoryAccumulator struct {
	history []float64
	maxSize int
}

func newSmoothedRTTHistoryAccumulator(maxSize int) *smoothedRTTHistoryAccumulator {
	if maxSize <= 0 {
		panic(fmt.Sprintf("Max size must be positive, got %d", maxSize))
	}
	return &smoothedRTTHistoryAccumulator{
		history: make([]float64, 0, maxSize),
		maxSize: maxSize,
	}
}

func (a *smoothedRTTHistoryAccumulator) add(rtt float64) {
	if len(a.history) >= a.maxSize {
		a.history = a.history[1:]
	}
	a.history = append(a.history, rtt)
}

func (a *smoothedRTTHistoryAccumulator) get() []float64 {
	return a.history
}

func (a *smoothedRTTHistoryAccumulator) len() int {
	return len(a.history)
}
