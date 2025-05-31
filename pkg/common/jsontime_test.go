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
