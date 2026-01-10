package common

import "testing"

func TestJsonTimeMarshal(t *testing.T) {
	jt := JSONTimeNow()
	b, err := jt.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var jt2 JSONTime
	err = jt2.UnmarshalJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if jt.String() != jt2.String() {
		t.Errorf("Times are not equal. jt=%v jt2=%v", jt.Time(), jt2.Time())
	}
}

func TestJSONTimeFromStringValid(t *testing.T) {
	t.Parallel()

	validTimeStr := "2024-01-15T10:30:00Z"
	jt := JSONTimeFromString(validTimeStr)

	if jt.Time().IsZero() {
		t.Error("Expected non-zero time from valid string")
	}

	if jt.String() != validTimeStr {
		t.Errorf("Time string mismatch: got %q, want %q", jt.String(), validTimeStr)
	}
}

func TestJSONTimeFromStringWithQuotes(t *testing.T) {
	t.Parallel()

	quotedTimeStr := `"2024-01-15T10:30:00Z"`
	jt := JSONTimeFromString(quotedTimeStr)

	if jt.Time().IsZero() {
		t.Error("Expected non-zero time from quoted string")
	}

	expected := "2024-01-15T10:30:00Z"
	if jt.String() != expected {
		t.Errorf("Time string mismatch: got %q, want %q", jt.String(), expected)
	}
}

func TestJSONTimeFromStringInvalid(t *testing.T) {
	t.Parallel()

	invalidTimeStr := "not-a-valid-time"
	jt := JSONTimeFromString(invalidTimeStr)

	if !jt.Time().IsZero() {
		t.Errorf("Expected zero time from invalid string, got %v", jt.Time())
	}
}

func TestJSONTimeFromStringEmpty(t *testing.T) {
	t.Parallel()

	jt := JSONTimeFromString("")

	if !jt.Time().IsZero() {
		t.Errorf("Expected zero time from empty string, got %v", jt.Time())
	}
}

func TestJSONTimeUnmarshalJSONInvalid(t *testing.T) {
	t.Parallel()

	var jt JSONTime
	invalidJSON := []byte(`"invalid-time-format"`)

	err := jt.UnmarshalJSON(invalidJSON)
	if err == nil {
		t.Error("Expected error when unmarshaling invalid time format")
	}

	if !jt.Time().IsZero() {
		t.Errorf("Expected zero time after failed unmarshal, got %v", jt.Time())
	}
}

func TestJSONTimeUnmarshalJSONEmpty(t *testing.T) {
	t.Parallel()

	var jt JSONTime
	emptyJSON := []byte(`""`)

	err := jt.UnmarshalJSON(emptyJSON)
	if err == nil {
		t.Error("Expected error when unmarshaling empty string")
	}
}

func TestJSONTimeUnmarshalJSONMalformed(t *testing.T) {
	t.Parallel()

	var jt JSONTime
	malformedJSON := []byte(`"2024-13-45T99:99:99Z"`)

	err := jt.UnmarshalJSON(malformedJSON)
	if err == nil {
		t.Error("Expected error when unmarshaling malformed date")
	}
}
