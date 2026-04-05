//go:build enterprise

package db

import "testing"

func TestContainsInvalidNameChars(t *testing.T) {
	const orgPunct = "'-_&.:()[]"
	const propPunct = "'-_.:()[]"

	tests := []struct {
		name             string
		input            string
		allowedPunct     string
		expectedPosition int
		expectedRune     rune
	}{
		{
			name:             "ValidLettersOnly",
			input:            "HelloWorld",
			allowedPunct:     "",
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "ValidWithDigits",
			input:            "Test123",
			allowedPunct:     "",
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "ValidWithSpaces",
			input:            "Hello World",
			allowedPunct:     "",
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "ValidOrgPunctuation",
			input:            "O'Reilly & Sons",
			allowedPunct:     orgPunct,
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "InvalidAtSign",
			input:            "Test@Name",
			allowedPunct:     "",
			expectedPosition: 4,
			expectedRune:     '@',
		},
		{
			name:             "AmpersandInvalidForProperty",
			input:            "Test&Name",
			allowedPunct:     propPunct,
			expectedPosition: 4,
			expectedRune:     '&',
		},
		{
			name:             "AmpersandValidForOrg",
			input:            "Test&Name",
			allowedPunct:     orgPunct,
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "EmptyString",
			input:            "",
			allowedPunct:     "",
			expectedPosition: -1,
			expectedRune:     0,
		},
		{
			name:             "UnicodeLetters",
			input:            "Caf\u00e9",
			allowedPunct:     "",
			expectedPosition: -1,
			expectedRune:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, r := containsInvalidNameChars(tt.input, tt.allowedPunct)
			if pos != tt.expectedPosition {
				t.Errorf("position = %d, want %d", pos, tt.expectedPosition)
			}
			if r != tt.expectedRune {
				t.Errorf("rune = %q, want %q", r, tt.expectedRune)
			}
		})
	}
}
