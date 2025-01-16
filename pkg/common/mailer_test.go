package common

import (
	"strings"
	"testing"
)

func TestEmailShortSubject(t *testing.T) {
	const shortSubject = "Some subject"
	req := &SupportRequest{
		Subject:  shortSubject,
		Category: "test",
	}

	if subject := req.EmailSubject(); !strings.HasSuffix(subject, shortSubject) {
		t.Errorf("Subject is %v", subject)
	}
}

func TestEmailLongSubject(t *testing.T) {
	const longSubject = "Lorem ipsum dolor sit amet, consectetur adipiscing elit. In ex libero, dignissim ut magna in laoreet."
	req := &SupportRequest{
		Subject:  longSubject,
		Category: "test",
	}

	if subject := req.EmailSubject(); !strings.HasSuffix(subject, "...") {
		t.Errorf("Subject is %v", subject)
	}
}
