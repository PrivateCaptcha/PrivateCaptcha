package puzzle

import "testing"

func TestInvalidSignature(t *testing.T) {
	s := new(signature)
	if uerr := s.UnmarshalBinary([]byte{1, 2}); uerr == nil {
		t.Fatal("Parsing succeeded with too short buffer")
	}
}
