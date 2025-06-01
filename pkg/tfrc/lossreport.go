package tfrc

import (
	"fmt"
	"time"
)

// lossReport holds fraction lost and report interval for RFC8083 averaging
type lossReport struct {
	fraction float64
	interval time.Duration
}

type lossReportAccumulator struct {
	lossReports []lossReport
	maxSize     int
}

func newLossReportAccumulator(maxSize int) *lossReportAccumulator {
	if maxSize <= 0 {
		panic(fmt.Sprintf("Max size must be positive, got %d", maxSize))
	}
	return &lossReportAccumulator{
		lossReports: make([]lossReport, 0, maxSize),
		maxSize:     maxSize,
	}
}
func (a *lossReportAccumulator) add(fraction float64, interval time.Duration) {
	if len(a.lossReports) >= a.maxSize {
		a.lossReports = a.lossReports[1:]
	}
	a.lossReports = append(a.lossReports, lossReport{fraction, interval})
}
func (a *lossReportAccumulator) get() []lossReport {
	return a.lossReports
}
func (a *lossReportAccumulator) len() int {
	return len(a.lossReports)
}
