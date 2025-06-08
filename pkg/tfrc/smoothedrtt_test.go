package tfrc

import (
	"reflect"
	"testing"
)

func TestNewSmoothedRTTHistoryAccumulator(t *testing.T) {
	tests := []struct {
		name    string
		maxSize int
		wantErr bool
	}{
		{
			name:    "valid max size",
			maxSize: 10,
			wantErr: false,
		},
		{
			name:    "zero max size",
			maxSize: 0,
			wantErr: true,
		},
		{
			name:    "negative max size",
			maxSize: -5,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.wantErr {
					t.Errorf("newSmoothedRTTHistoryAccumulator() panic = %v, wantErr %v", r, tt.wantErr)
				}
			}()

			acc := newSmoothedRTTHistoryAccumulator(tt.maxSize)
			if !tt.wantErr {
				if acc == nil {
					t.Error("newSmoothedRTTHistoryAccumulator() returned nil, expected non-nil")
				}
				if acc.maxSize != tt.maxSize {
					t.Errorf("newSmoothedRTTHistoryAccumulator() maxSize = %v, want %v", acc.maxSize, tt.maxSize)
				}
				if cap(acc.history) != tt.maxSize {
					t.Errorf("newSmoothedRTTHistoryAccumulator() capacity = %v, want %v", cap(acc.history), tt.maxSize)
				}
			}
		})
	}
}

func TestSmoothedRTTHistoryAccumulator_Add(t *testing.T) {
	t.Run("add below capacity", func(t *testing.T) {
		acc := newSmoothedRTTHistoryAccumulator(3)

		acc.add(0.1)
		acc.add(0.2)

		if len(acc.history) != 2 {
			t.Errorf("add() length = %v, want %v", len(acc.history), 2)
		}

		expected := []float64{0.1, 0.2}

		if !reflect.DeepEqual(acc.history, expected) {
			t.Errorf("add() history = %v, want %v", acc.history, expected)
		}
	})

	t.Run("add beyond capacity", func(t *testing.T) {
		acc := newSmoothedRTTHistoryAccumulator(2)

		acc.add(0.1)
		acc.add(0.2)
		acc.add(0.3)

		if len(acc.history) != 2 {
			t.Errorf("add() length = %v, want %v", len(acc.history), 2)
		}

		// First value should be removed, keeping only the last 2
		expected := []float64{0.2, 0.3}

		if !reflect.DeepEqual(acc.history, expected) {
			t.Errorf("add() history = %v, want %v", acc.history, expected)
		}
	})
}

func TestSmoothedRTTHistoryAccumulator_Get(t *testing.T) {
	acc := newSmoothedRTTHistoryAccumulator(3)

	// Empty history initially
	if history := acc.get(); len(history) != 0 {
		t.Errorf("get() on empty accumulator returned %v, want empty slice", history)
	}

	// Add some values
	acc.add(0.1)
	acc.add(0.2)

	expected := []float64{0.1, 0.2}

	history := acc.get()
	if !reflect.DeepEqual(history, expected) {
		t.Errorf("get() history = %v, want %v", history, expected)
	}

	// Verify that get() doesn't modify the original data
	if !reflect.DeepEqual(acc.history, expected) {
		t.Errorf("get() modified original history: %v, want %v", acc.history, expected)
	}
}

func TestSmoothedRTTHistoryAccumulator_Len(t *testing.T) {
	acc := newSmoothedRTTHistoryAccumulator(5)

	// Empty initially
	if acc.len() != 0 {
		t.Errorf("len() = %v, want %v", acc.len(), 0)
	}

	// Add some values
	acc.add(0.1)
	acc.add(0.2)
	acc.add(0.3)

	if acc.len() != 3 {
		t.Errorf("len() = %v, want %v", acc.len(), 3)
	}

	// Add beyond capacity
	acc.add(0.4)
	acc.add(0.5)
	acc.add(0.6)

	if acc.len() != 5 {
		t.Errorf("len() = %v, want %v", acc.len(), 5)
	}
}

func TestSmoothedRTTHistoryAccumulator_TrendAnalysis(t *testing.T) {
	acc := newSmoothedRTTHistoryAccumulator(5)

	// Test with ascending RTT values (increasing trend)
	acc.add(0.1)
	acc.add(0.15)
	acc.add(0.2)
	acc.add(0.25)
	acc.add(0.3)

	history := acc.get()
	if history[0] >= history[len(history)-1] {
		t.Errorf("Failed to preserve increasing trend: %v", history)
	}

	// Clear and test with descending values (decreasing trend)
	acc = newSmoothedRTTHistoryAccumulator(5)
	acc.add(0.5)
	acc.add(0.4)
	acc.add(0.3)
	acc.add(0.2)
	acc.add(0.1)

	history = acc.get()
	if history[0] <= history[len(history)-1] {
		t.Errorf("Failed to preserve decreasing trend: %v", history)
	}
}
