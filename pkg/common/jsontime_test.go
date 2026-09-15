package common

import (
	"testing"
	"time"
)

func TestJsonTimeMarshal(t *testing.T) {
	jt := JSONTime(time.Now().UTC())
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
