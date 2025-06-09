package tfrc

import (
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		init    int
		min     int
		max     int
		wantErr bool
	}{
		{
			name:    "valid parameters",
			init:    1000,
			min:     500,
			max:     4000,
			wantErr: false,
		},
		{
			name:    "init below min",
			init:    400,
			min:     500,
			max:     4000,
			wantErr: true,
		},
		{
			name:    "init above max",
			init:    5000,
			min:     500,
			max:     4000,
			wantErr: true,
		},
		{
			name:    "negative min",
			init:    1000,
			min:     -500,
			max:     4000,
			wantErr: true,
		},
		{
			name:    "min greater than max",
			init:    1000,
			min:     2000,
			max:     1000,
			wantErr: true,
		},
		{
			name:    "min too low",
			init:    1000,
			min:     100,
			max:     4000,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.wantErr {
					t.Errorf("New() panic = %v, wantErr %v", r, tt.wantErr)
				}
			}()

			tfrc := New(tt.init, tt.min, tt.max)
			if tfrc == nil && !tt.wantErr {
				t.Errorf("New() returned nil, expected non-nil")
			}

			if !tt.wantErr {
				if tfrc.currentBitrate != tt.init {
					t.Errorf("New() currentBitrate = %v, want %v", tfrc.currentBitrate, tt.init)
				}
				if tfrc.minBitrate != tt.min {
					t.Errorf("New() minBitrate = %v, want %v", tfrc.minBitrate, tt.min)
				}
				if tfrc.maxBitrate != tt.max {
					t.Errorf("New() maxBitrate = %v, want %v", tfrc.maxBitrate, tt.max)
				}
			}
		})
	}
}

func TestTfrc_GetMethods(t *testing.T) {
	tfrc := New(1000, 500, 4000)

	// Set some known values for testing
	tfrc.pSample = 0.05
	tfrc.rttSampe = 0.1
	tfrc.smoothedRTT = 0.12

	t.Run("GetLastFraction", func(t *testing.T) {
		if got := tfrc.GetLastFraction(); got != 0.05 {
			t.Errorf("GetLastFraction() = %v, want %v", got, 0.05)
		}
	})

	t.Run("GetRttSample", func(t *testing.T) {
		if got := tfrc.GetRttSample(); got != 0.1 {
			t.Errorf("GetRttSample() = %v, want %v", got, 0.1)
		}
	})

	t.Run("GetSmoothedRTT", func(t *testing.T) {
		if got := tfrc.GetSmoothedRTT(); got != 0.12 {
			t.Errorf("GetSmoothedRTT() = %v, want %v", got, 0.12)
		}
	})
}

func TestTfrc_PreProcessRTCP(t *testing.T) {
	tfrc := New(1000, 500, 4000)

	// Prepare RTCP data
	now := time.Now()
	var lsr uint32 = 0x12345678   // Example LSR value
	var delay uint32 = 0x00001234 // Example delay value
	var fractionLost uint8 = 25   // About 10% loss (25/256)

	// Process RTCP data
	tfrc.PreProcessRTCP(now, lsr, delay, fractionLost)

	// Verify loss fraction was processed
	expectedLossFraction := float64(fractionLost) / 256.0
	if tfrc.GetLastFraction() != expectedLossFraction {
		t.Errorf("PreProcessRTCP() pSample = %v, want %v", tfrc.GetLastFraction(), expectedLossFraction)
	}

	// Verify we have a valid RTT sample
	if tfrc.GetRttSample() < 0 {
		t.Errorf("PreProcessRTCP() RTT sample should be non-negative, got %v", tfrc.GetRttSample())
	}

	// Verify we have a valid smoothed RTT
	if tfrc.GetSmoothedRTT() <= 0 {
		t.Errorf("PreProcessRTCP() smoothed RTT should be positive, got %v", tfrc.GetSmoothedRTT())
	}

	// Verify the loss report was added
	if tfrc.lossReports.len() == 0 {
		t.Error("PreProcessRTCP() did not add loss report")
	}

	// Verify RTT history was updated
	if tfrc.smoothedRTTHistory.len() == 0 {
		t.Error("PreProcessRTCP() did not update RTT history")
	}
}

func TestTfrc_computeRTTTrend(t *testing.T) {
	tests := []struct {
		name     string
		rttData  []float64
		expected int
	}{
		{
			name:     "empty history",
			rttData:  []float64{},
			expected: 1, // Default when insufficient data
		},
		{
			name:     "single value",
			rttData:  []float64{0.1},
			expected: 1, // Default when insufficient data
		},
		{
			name:     "two values - not enough for trend",
			rttData:  []float64{0.1, 0.2},
			expected: 1, // Default when insufficient data
		},
		{
			name:     "increasing trend",
			rttData:  []float64{0.1, 0.12, 0.15, 0.2, 0.25, 0.3},
			expected: 1, // Increasing trend
		},
		{
			name:     "decreasing trend",
			rttData:  []float64{0.3, 0.25, 0.2, 0.15, 0.12, 0.1},
			expected: -1, // Decreasing trend
		},
		{
			name:     "stable trend",
			rttData:  []float64{0.2, 0.21, 0.19, 0.205, 0.195, 0.2},
			expected: 0, // Stable trend
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tfrc := New(1000, 500, 4000)

			// Add RTT data to history
			for _, rtt := range tt.rttData {
				tfrc.smoothedRTTHistory.add(rtt)
			}

			// Compute trend
			trend := tfrc.computeRTTTrend()

			if trend != tt.expected {
				t.Errorf("computeRTTTrend() = %v, want %v", trend, tt.expected)
			}
		})
	}
}

func TestTfrc_computeLossEventRate(t *testing.T) {
	tfrc := New(1000, 500, 4000)

	// Case 1: No loss reports
	if rate := tfrc.computeLossEventRate(); rate != 0 {
		t.Errorf("computeLossEventRate() with no reports = %v, want 0", rate)
	}

	// Case 2: Some loss reports
	tfrc.lossReports.add(0.1, 100*time.Millisecond)
	tfrc.lossReports.add(0.2, 200*time.Millisecond)

	// Expected: (0.1*0.1 + 0.2*0.2) / (0.1 + 0.2) = 0.03/0.3 = 0.1
	expectedRate := (0.1*0.1 + 0.2*0.2) / (0.1 + 0.2)
	rate := tfrc.computeLossEventRate()

	// Allow small floating point difference
	if abs(rate-expectedRate) > 0.001 {
		t.Errorf("computeLossEventRate() = %v, want approximately %v", rate, expectedRate)
	}
}

func TestTfrc_smoothRate(t *testing.T) {
	tfrc := New(1000, 500, 4000)

	tests := []struct {
		name     string
		current  int
		target   int
		expected int
	}{
		{
			name:     "increase within bounds",
			current:  1000,
			target:   2000,
			expected: 1200, // 1000 + 0.2*(2000-1000) = 1000 + 200 = 1200
		},
		{
			name:     "decrease within bounds",
			current:  2000,
			target:   1000,
			expected: 1800, // 2000 + 0.2*(1000-2000) = 2000 - 200 = 1800
		},
		{
			name:     "would exceed max",
			current:  3500,
			target:   5000,
			expected: 3800, // Limited to max 4000
		},
		{
			name:     "would go below min",
			current:  600,
			target:   0,
			expected: 500, // Limited to min 500
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tfrc.smoothRate(tt.current, tt.target)
			if result != tt.expected {
				t.Errorf("smoothRate(%d, %d) = %d, want %d",
					tt.current, tt.target, result, tt.expected)
			}
		})
	}
}

func TestTfrc_ComputeTFRCBitrate(t *testing.T) {
	tfrc := New(1000, 500, 4000)

	// Test 1: Zero loss
	t.Run("zero loss", func(t *testing.T) {
		// Set up the state for zero loss
		tfrc.smoothedRTT = 0.1
		// Empty loss reports means zero loss rate

		// Add some RTT history with stable trend
		tfrc.smoothedRTTHistory.add(0.1)
		tfrc.smoothedRTTHistory.add(0.1)
		tfrc.smoothedRTTHistory.add(0.1)

		bitrate := tfrc.ComputeTFRCBitrate()

		// With zero loss and stable RTT, we should target max bitrate
		// But we'll only move 20% of the way there
		expected := 1000 + int(0.2*float64(4000-1000))
		if abs(float64(bitrate-expected)) > 5 { // Allow small difference due to floating point
			t.Errorf("ComputeTFRCBitrate() with zero loss = %d, want approximately %d",
				bitrate, expected)
		}
	})

	// Test 2: High loss
	t.Run("high loss", func(t *testing.T) {
		// Reset the TFRC controller
		tfrc = New(1000, 500, 4000)

		// Set up the state for high loss
		tfrc.smoothedRTT = 0.1

		// Add loss reports
		tfrc.lossReports.add(0.25, 100*time.Millisecond) // 25% loss

		bitrate := tfrc.ComputeTFRCBitrate()

		// With high loss, the bitrate should decrease
		if bitrate >= 1000 {
			t.Errorf("ComputeTFRCBitrate() with high loss = %d, should be less than initial 1000",
				bitrate)
		}
	})
}

func TestNowMiddle32(t *testing.T) {
	// Test 1: Basic format check
	val := nowMiddle32()

	// The value should be non-zero
	if val == 0 {
		t.Error("nowMiddle32() returned 0")
	}

	// Test 2: Components should be 16 bits each
	secs := (val >> 16)
	frac := (val & 0xFFFF)

	if secs > 0xFFFF {
		t.Errorf("Seconds component exceeds 16 bits: %d", secs)
	}

	if frac > 0xFFFF {
		t.Errorf("Fraction component exceeds 16 bits: %d", frac)
	}

	// Test 3: Values should generally increase over time
	time.Sleep(10 * time.Millisecond)
	val2 := nowMiddle32()

	// Calculate difference with potential wrap-around
	diff := int64(val2) - int64(val)
	if diff <= 0 {
		// Only acceptable if we're at a 16-bit seconds boundary rollover
		secs1 := val >> 16
		secs2 := val2 >> 16
		if secs1 != 0xFFFF || secs2 != 0 {
			t.Errorf("nowMiddle32() did not increase as expected: %d -> %d", val, val2)
		}
	}

	// Test 4: Check a longer interval
	time.Sleep(100 * time.Millisecond)
	val3 := nowMiddle32()

	// The value should increase noticeably over 100ms
	diff = int64(val3) - int64(val2)
	if diff <= 0 {
		// Only acceptable if we're at a 16-bit seconds boundary rollover
		secs2 := val2 >> 16
		secs3 := val3 >> 16
		if secs2 != 0xFFFF || secs3 != 0 {
			t.Errorf("nowMiddle32() did not increase after 100ms: %d -> %d", val2, val3)
		}
	}

	// Test 5: Verify value corresponds roughly to current time
	currentTime := time.Now().UTC()
	expectedSecs := uint64(currentTime.Unix()) + ntpEpochOffset
	expectedSecs16 := uint32(expectedSecs & 0xFFFF)

	// Allow small difference due to execution time
	secsDiff := absDiff(secs, expectedSecs16)
	if secsDiff > 1 {
		t.Errorf("Seconds component doesn't match current time: got %d, expected ~%d",
			secs, expectedSecs16)
	}
}

// Helper function for absolute difference
func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// Helper function for floating point comparison
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
