package tfrc

import (
	"fmt"
	"math"
	"time"
)

const (
	maxSmoothedRTTReports = 10

	// maxLossReports is the maximum number of loss reports to keep in the RFC8083 loss event window
	// TODO: CB_INTERVAL = ceil(3*min(max(10*G*Tf, 10*Tr, 3*Tdr), max(15, 3*Td))/(3*Tdr)) as per RFC8083 4.2
	maxLossReports = 10

	ntpEpochOffset = 2208988800
)

type Tfrc struct {
	// TFRC parameters
	smoothedRTT        float64
	smoothedRTTHistory *smoothedRTTHistoryAccumulator

	// pSample last fraction lost sample
	pSample float64

	// rttSample
	rttSampe float64

	// TTR parameters
	ttrParamsAlpha float64
	avgPacketSize  float64

	// rate limits Kbps
	minBitrate     int
	maxBitrate     int
	currentBitrate int

	// RFC8083 loss event window
	lossReports        *lossReportAccumulator
	lastLossReportTime time.Time
}

func New(init, min, max int) *Tfrc {
	if init < min || init > max {
		panic(fmt.Sprintf("Initial bitrate %d must be between min %d and max %d", init, min, max))
	}
	if min <= 0 || max <= 0 || init <= 0 {
		panic(fmt.Sprintf("Bitrate limits must be positive: min %d, max %d", min, max))
	}
	if min > max {
		panic(fmt.Sprintf("Min bitrate %d cannot be greater than max bitrate %d", min, max))
	}
	if init < 500 || min < 500 || max < 500 {
		panic(fmt.Sprintf("Bitrate must be at least 500 Kbps: init %d, min %d, max %d", init, min, max))
	}

	currentBitrate := init
	minBitrate := min
	maxBitrate := max

	smoothedRTTHistoryAccumulator := newSmoothedRTTHistoryAccumulator(maxSmoothedRTTReports)
	if smoothedRTTHistoryAccumulator == nil {
		panic("Failed to create smoothedRTTHistoryAccumulator")
	}

	lossReportAccumulator := newLossReportAccumulator(maxLossReports)
	if lossReportAccumulator == nil {
		panic("Failed to create lossReportAccumulator")
	}

	return &Tfrc{
		smoothedRTTHistory: smoothedRTTHistoryAccumulator,
		smoothedRTT:        0.1,                   // initial RTT estimate
		ttrParamsAlpha:     0.2,                   // smoothing factor
		avgPacketSize:      1200.0,                // average RTP payload size
		minBitrate:         minBitrate,            // Kbps
		maxBitrate:         maxBitrate,            // Kbps
		pSample:            0.0,                   // last fraction lost sample
		rttSampe:           0.0,                   // last RTT sample
		currentBitrate:     currentBitrate,        // initial bitrate in Kbps
		lossReports:        lossReportAccumulator, // RFC8083 loss event window
		lastLossReportTime: time.Now(),
	}
}

// PreProcessRTCP processes RTCP packets before computing bitrate
func (t *Tfrc) PreProcessRTCP(now time.Time, lsr, delay uint32, fractionLost uint8) {
	// 1. Compute RTT sample from LSR and delay
	rttSampe := t.computeRTTSample(lsr, delay)

	// 2. Update smoothed RTT ring buffer
	t.updateSmoothedRTT(rttSampe)

	// 3. compute loss event rate and record loss event
	t.recordLossEvent(fractionLost, now)
}

// recordLossEvent appends fractionLost sample and interval
func (t *Tfrc) recordLossEvent(fractionLost uint8, now time.Time) {
	t.pSample = float64(fractionLost) / 256.0
	interval := now.Sub(t.lastLossReportTime)
	t.lossReports.add(t.pSample, interval)
	t.lastLossReportTime = now
}

// Return the last recorded fraction lost
func (t *Tfrc) GetLastFraction() float64 {
	return t.pSample
}

// computeLossEventRate calculates p via RFC8083 time-weighted average
func (t *Tfrc) computeLossEventRate() float64 {
	var num, den float64
	for _, r := range t.lossReports.get() {
		sec := r.interval.Seconds()
		num += r.fraction * sec
		den += sec
	}
	if den <= 0 {
		return 0
	}
	return num / den
}

// recordRTT stores smoothedRTT in history
func (t *Tfrc) recordRTT(rtt float64) {
	t.smoothedRTTHistory.add(rtt)
}

// computeRTTTrend returns -1 (decreasing), 0 (stable), or 1 (increasing)
// TODO: better approach to detect RTT trend
func (t *Tfrc) computeRTTTrend() int {
	n := t.smoothedRTTHistory.len()
	if n <= 2 {
		return 1 // hold for a while until we have enough data
	}
	half := n / 2
	var oldSum, newSum float64
	for i := 0; i < half; i++ {
		oldSum += t.smoothedRTTHistory.get()[i]
	}
	for i := half; i < n; i++ {
		newSum += t.smoothedRTTHistory.get()[i]
	}
	oldAvg := oldSum / float64(half)
	newAvg := newSum / float64(n-half)
	// Thresholds ±20%
	if newAvg > oldAvg*1.2 {
		return 1
	} else if newAvg < oldAvg*0.8 {
		return -1
	}
	return 0
}

// ComputeRTTSample computes RTT sample based on LSR and delay
func (t *Tfrc) computeRTTSample(lsr, delay uint32) float64 {
	now32 := nowMiddle32()
	rtt := max(now32-(lsr+delay), 0)
	t.rttSampe = float64(rtt) / 65536.0
	return t.rttSampe
}

// GetRttSample returns the last RTT sample
func (t *Tfrc) GetRttSample() float64 {
	return t.rttSampe
}

// updateSmoothedRTT applies exponential smoothing to the RTT sample
// Smooth RTT: R <- 0.8·R + 0.2·RTT_sample (RTT_sample from LSR/DLSR)
func (t *Tfrc) updateSmoothedRTT(rtt float64) {
	t.smoothedRTT = (1-t.ttrParamsAlpha)*t.smoothedRTT + t.ttrParamsAlpha*rtt
	t.recordRTT(t.smoothedRTT)
}

// Return the current smoothed RTT
func (t *Tfrc) GetSmoothedRTT() float64 {
	return t.smoothedRTT
}

// NowMiddle32 returns the "LSR"‐style 32‐bit value:
// upper 16 bits = least significant 16 bits of seconds since NTP epoch
// lower 16 bits = most significant 16 bits of the fractional second
//
//nolint:gosec
func nowMiddle32() uint32 {
	t := time.Now().UTC()
	// Full seconds since NTP epoch
	secs := uint64(t.Unix()) + ntpEpochOffset
	// Full 32‐bit fraction of a second
	frac := uint64(t.Nanosecond()) * (1 << 32) / 1e9

	// Take low 16 bits of secs, high 16 bits of frac
	secs16 := uint32(secs & 0xFFFF)
	frac16 := uint32(frac >> 16)

	return (secs16 << 16) | frac16
}

// computeTFRCBitrate computes the TFRC bitrate based on RFC 5348 and RFC 8083
func (t *Tfrc) ComputeTFRCBitrate() int {
	var trend int

	fmt.Printf("Current bitrate: %d Kbps\n", t.currentBitrate)

	// 1. Calculate loss-event rate p via RFC8083
	p := t.computeLossEventRate()
	fmt.Printf("Loss event rate p=%.4f\n", p)

	// 2. Check if loss is zero
	// TODO: improve this approach: if RTT is stable/decreasing, cautiously increase bitrate 5%?
	if p <= 0 {
		trend = t.computeRTTTrend()
		var target int
		switch trend {
		case 1:
			// RTT increasing: hold current bitrate
			fmt.Printf("RTT increasing, holding current bitrate: %d Kbps\n", t.currentBitrate)
			return t.currentBitrate
		case -1, 0:
			// RTT stable or decreasing: ramp toward ceiling
			fmt.Printf("RTT stable or decreasing, ramping toward max bitrate: %d Kbps\n", t.maxBitrate)
			target = t.maxBitrate
		}
		return t.smoothRate(t.currentBitrate, target)
	}

	// 3. Calculate RTO and terms
	R := t.smoothedRTT
	rto := 4 * R
	term1 := R * math.Sqrt(2*p/3)
	term2 := rto * (3 * math.Sqrt(3*p/8) * p * (1 + 32*p*p))
	if term1+term2 <= 0 {
		fmt.Println("Invalid TFRC parameters, using current bitrate")
		return t.currentBitrate
	}
	fmt.Printf("RTT=%.3fs, p=%.4f, RTO=%.3fs, term1=%.4f, term2=%.4f\n", t.smoothedRTT, p, rto, term1, term2)

	// 4. Throughput in bytes/sec
	x := t.avgPacketSize / (term1 + term2)
	fmt.Printf("TFRC x=%.4f bytes/sec\n", x)
	// Convert to Kbps
	targetKbps := int(x * 8 / 1000)
	fmt.Printf("TFRC target=%d Kbps\n", targetKbps)

	// TODO: 5. clamp targetKbps to min/max bitrate?

	// 6. Smooth toward target
	return t.smoothRate(t.currentBitrate, targetKbps)
}

// smoothRate applies exponential smoothing toward target
func (t *Tfrc) smoothRate(current, target int) int {
	newRate := int(float64(current) + t.ttrParamsAlpha*(float64(target)-float64(current)))
	fmt.Printf("TFRC smoothed=%d Kbps\n", newRate)
	if newRate < t.minBitrate {
		newRate = t.minBitrate
	} else if newRate > t.maxBitrate {
		newRate = t.maxBitrate
	}
	fmt.Printf("TFRC clamped=%d Kbps\n", newRate)
	t.currentBitrate = newRate
	return newRate
}
