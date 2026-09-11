package common

import (
	"strings"
	"time"
)

type VerifyClient string

const (
	VerifyClientUnknown VerifyClient = "unknown"
	VerifyClientJava    VerifyClient = "java"
	VerifyClientPHP     VerifyClient = "php"
	VerifyClientGo      VerifyClient = "go"
	VerifyClientDotNet  VerifyClient = "dotnet"
	VerifyClientRuby    VerifyClient = "ruby"
	VerifyClientPython  VerifyClient = "py"
	VerifyClientJS      VerifyClient = "js"
	VerifyClientCurl    VerifyClient = "curl"
	VerifyClientForm    VerifyClient = "form"
	VerifyClientPortal  VerifyClient = "portal"
)

func NormalizeVerifyClient(client string) VerifyClient {
	switch client {
	case "java":
		return VerifyClientJava
	case "php":
		return VerifyClientPHP
	case "go":
		return VerifyClientGo
	case "dotnet":
		return VerifyClientDotNet
	case "ruby":
		return VerifyClientRuby
	case "py":
		return VerifyClientPython
	case "js":
		return VerifyClientJS
	case "curl", "libcurl":
		return VerifyClientCurl
	case "form":
		return VerifyClientForm
	case "portal":
		return VerifyClientPortal
	default:
		return VerifyClientUnknown
	}
}

func VerifyClientFromUserAgent(userAgent string) VerifyClient {
	const prefix = "private-captcha-"
	value, ok := strings.CutPrefix(userAgent, prefix)
	if !ok {
		value = userAgent
	}
	if client, _, ok := strings.Cut(value, "/"); ok {
		value = client
	} else {
		value = value[:min(len(value), 25)]
	}

	return NormalizeVerifyClient(value)
}

type AccessRecord struct {
	Fingerprint  TFingerprint
	UserID       int32
	OrgID        int32
	PropertyID   int32
	RuleID       int32
	Timestamp    time.Time
	PuzzleID     uint64
	ExpiresAt    time.Time
	IPFamily     uint8
	IPPrefix     uint64
	Browser      string
	BrowserMajor uint16
	OS           string
	Device       string
}

type VerifyRecord struct {
	UserID     int32
	OrgID      int32
	PropertyID int32
	PuzzleID   uint64
	Timestamp  time.Time
	ExpiresAt  time.Time
	UserAgent  string
	Status     int8
}

type FormSubmitRecord struct {
	UserID    int32
	OrgID     int32
	FormID    int32
	Timestamp time.Time
	Status    int8
}
