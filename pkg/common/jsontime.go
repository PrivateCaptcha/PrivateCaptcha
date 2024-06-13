package common

import (
	"log/slog"
	"strings"
	"time"
)

// IETF RFC 3339 defines a profile of ISO 8601
const jsonTimeLayout = time.RFC3339

// JSONTime is the time.Time with JSON marshal and unmarshal capability
type JSONTime time.Time

// JSONTimeNow() is an alias to time.Now() casted to JSONTime
func JSONTimeNow() JSONTime {
	return JSONTime(time.Now().UTC())
}

func JSONTimeNowAdd(d time.Duration) JSONTime {
	return JSONTime(time.Now().Add(d).UTC())
}

func JSONTimeFromString(s string) JSONTime {
	s = strings.Trim(s, `"`)
	nt, err := time.Parse(jsonTimeLayout, s)
	if err != nil {
		slog.Error("Failed to parse a json time", "string", s, ErrAttr(err))
		return JSONTime{}
	}

	return JSONTime(nt)
}

// UnmarshalJSON will unmarshal using 2006-01-02T15:04:05+07:00 layout
func (t *JSONTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	nt, err := time.Parse(jsonTimeLayout, s)
	if err != nil {
		slog.Error("Failed to unmarshal a json time", "string", s, ErrAttr(err))
		return err
	}
	*t = JSONTime(nt)
	return nil
}

// Time returns builtin time.Time for current JSONTime
func (t JSONTime) Time() time.Time {
	return time.Time(t)
}

// MarshalJSON will marshal using 2006-01-02T15:04:05+07:00 layout
func (t *JSONTime) MarshalJSON() ([]byte, error) {
	return []byte(t.String()), nil
}

// String returns the time in the custom format
func (t JSONTime) String() string {
	ct := time.Time(t)
	return ct.Format(jsonTimeLayout)
}

func (t JSONTime) LogValue() slog.Value {
	return slog.StringValue(t.String())
}
