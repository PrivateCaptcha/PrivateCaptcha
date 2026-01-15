package common

import "testing"

func TestTimePeriodString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		period   TimePeriod
		expected string
	}{
		{TimePeriodToday, "today"},
		{TimePeriodWeek, "week"},
		{TimePeriodMonth, "month"},
		{TimePeriodYear, "year"},
		{TimePeriod(100), "unknown"},
		{TimePeriod(-1), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.period.String()
			if result != tt.expected {
				t.Errorf("TimePeriod(%d).String() = %q, want %q", tt.period, result, tt.expected)
			}
		})
	}
}
