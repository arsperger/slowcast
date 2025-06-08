package tfrc

import (
	"reflect"
	"testing"
	"time"
)

func TestNewLossReportAccumulator(t *testing.T) {
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
					t.Errorf("newLossReportAccumulator() panic = %v, wantErr %v", r, tt.wantErr)
				}
			}()

			acc := newLossReportAccumulator(tt.maxSize)
			if !tt.wantErr {
				if acc == nil {
					t.Error("newLossReportAccumulator() returned nil, expected non-nil")
				}
				if acc.maxSize != tt.maxSize {
					t.Errorf("newLossReportAccumulator() maxSize = %v, want %v", acc.maxSize, tt.maxSize)
				}
				if cap(acc.lossReports) != tt.maxSize {
					t.Errorf("newLossReportAccumulator() capacity = %v, want %v", cap(acc.lossReports), tt.maxSize)
				}
			}
		})
	}
}

func TestLossReportAccumulator_Add(t *testing.T) {
	t.Run("add below capacity", func(t *testing.T) {
		acc := newLossReportAccumulator(3)

		acc.add(0.1, 100*time.Millisecond)
		acc.add(0.2, 200*time.Millisecond)

		if len(acc.lossReports) != 2 {
			t.Errorf("add() length = %v, want %v", len(acc.lossReports), 2)
		}

		expected := []lossReport{
			{fraction: 0.1, interval: 100 * time.Millisecond},
			{fraction: 0.2, interval: 200 * time.Millisecond},
		}

		if !reflect.DeepEqual(acc.lossReports, expected) {
			t.Errorf("add() reports = %v, want %v", acc.lossReports, expected)
		}
	})

	t.Run("add beyond capacity", func(t *testing.T) {
		acc := newLossReportAccumulator(2)

		acc.add(0.1, 100*time.Millisecond)
		acc.add(0.2, 200*time.Millisecond)
		acc.add(0.3, 300*time.Millisecond)

		if len(acc.lossReports) != 2 {
			t.Errorf("add() length = %v, want %v", len(acc.lossReports), 2)
		}

		// First report should be removed, keeping only the last 2
		expected := []lossReport{
			{fraction: 0.2, interval: 200 * time.Millisecond},
			{fraction: 0.3, interval: 300 * time.Millisecond},
		}

		if !reflect.DeepEqual(acc.lossReports, expected) {
			t.Errorf("add() reports = %v, want %v", acc.lossReports, expected)
		}
	})
}

func TestLossReportAccumulator_Get(t *testing.T) {
	acc := newLossReportAccumulator(3)

	// Empty reports initially
	if reports := acc.get(); len(reports) != 0 {
		t.Errorf("get() on empty accumulator returned %v, want empty slice", reports)
	}

	// Add some reports
	acc.add(0.1, 100*time.Millisecond)
	acc.add(0.2, 200*time.Millisecond)

	expected := []lossReport{
		{fraction: 0.1, interval: 100 * time.Millisecond},
		{fraction: 0.2, interval: 200 * time.Millisecond},
	}

	reports := acc.get()
	if !reflect.DeepEqual(reports, expected) {
		t.Errorf("get() reports = %v, want %v", reports, expected)
	}

	// Make sure the get() method doesn't modify the original data
	if !reflect.DeepEqual(acc.lossReports, expected) {
		t.Errorf("get() modified original reports: %v, want %v", acc.lossReports, expected)
	}
}

func TestLossReportAccumulator_Len(t *testing.T) {
	acc := newLossReportAccumulator(5)

	// Empty initially
	if acc.len() != 0 {
		t.Errorf("len() = %v, want %v", acc.len(), 0)
	}

	// Add some reports
	acc.add(0.1, 100*time.Millisecond)
	acc.add(0.2, 200*time.Millisecond)
	acc.add(0.3, 300*time.Millisecond)

	if acc.len() != 3 {
		t.Errorf("len() = %v, want %v", acc.len(), 3)
	}

	// Add beyond capacity
	acc.add(0.4, 400*time.Millisecond)
	acc.add(0.5, 500*time.Millisecond)
	acc.add(0.6, 600*time.Millisecond)

	if acc.len() != 5 {
		t.Errorf("len() = %v, want %v", acc.len(), 5)
	}
}
