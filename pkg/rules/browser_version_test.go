package rules

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/medama-io/go-useragent"
)

type browserVersionCase struct {
	name string
	key  BrowserVersionKey
	ua   string
}

func browserVersionCases(major int) []browserVersionCase {
	return []browserVersionCase{
		{
			name: "ChromeWindows",
			key:  BrowserVersionKey{Browser: BrowserChrome, Platform: PlatformWindows},
			ua:   fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36", major),
		},
		{
			name: "ChromeMacOS",
			key:  BrowserVersionKey{Browser: BrowserChrome, Platform: PlatformMacOS},
			ua:   fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36", major),
		},
		{
			name: "ChromeLinux",
			key:  BrowserVersionKey{Browser: BrowserChrome, Platform: PlatformLinux},
			ua:   fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36", major),
		},
		{
			name: "FirefoxWindows",
			key:  BrowserVersionKey{Browser: BrowserFirefox, Platform: PlatformWindows},
			ua:   fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:%d.0) Gecko/20100101 Firefox/%d.0", major, major),
		},
		{
			name: "FirefoxMacOS",
			key:  BrowserVersionKey{Browser: BrowserFirefox, Platform: PlatformMacOS},
			ua:   fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:%d.0) Gecko/20100101 Firefox/%d.0", major, major),
		},
		{
			name: "FirefoxLinux",
			key:  BrowserVersionKey{Browser: BrowserFirefox, Platform: PlatformLinux},
			ua:   fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64; rv:%d.0) Gecko/20100101 Firefox/%d.0", major, major),
		},
		{
			name: "ChromeAndroidWebView",
			key:  BrowserVersionKey{Browser: BrowserChrome, Platform: PlatformAndroid},
			ua:   fmt.Sprintf("Mozilla/5.0 (Linux; Android 14; Pixel 8 Build/UQ1A.240205.004; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/%d.0.0.0 Mobile Safari/537.36", major),
		},
		{
			name: "ChromeIOS",
			key:  BrowserVersionKey{Browser: BrowserChrome, Platform: PlatformIOS},
			ua:   fmt.Sprintf("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/%d.0.0.0 Mobile/15E148 Safari/604.1", major),
		},
		{
			name: "FirefoxAndroid",
			key:  BrowserVersionKey{Browser: BrowserFirefox, Platform: PlatformAndroid},
			ua:   fmt.Sprintf("Mozilla/5.0 (Android 14; Mobile; rv:%d.0) Gecko/%d.0 Firefox/%d.0", major, major, major),
		},
		{
			name: "ChromeAndroidTablet",
			key:  BrowserVersionKey{Browser: BrowserChrome, Platform: PlatformAndroid},
			ua:   fmt.Sprintf("Mozilla/5.0 (Linux; Android 14; Pixel Tablet) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36", major),
		},
	}
}

func newBrowserVersionRule(threshold int32) *dbgen.DifficultyRule {
	return &dbgen.DifficultyRule{
		ID:                42,
		ConditionProperty: dbgen.RuleConditionPropertyBrowserVersion,
		ConditionOperator: dbgen.RuleConditionOperatorMore,
		ConditionValueInt: pgtype.Int4{Int32: threshold, Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       100,
		Enabled:           true,
	}
}

func browserVersionRequest(ua string) *RequestInfo {
	return newTestRequestInfo(ua, netip.MustParseAddr("1.2.3.4"))
}

func TestBrowserVersionCompileValidation(t *testing.T) {
	compiler := NewRulesCompiler(useragent.NewParser())
	if _, err := compiler.CompileRule(t.Context(), newBrowserVersionRule(1)); err != nil {
		t.Fatalf("valid rule failed to compile: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*dbgen.DifficultyRule)
		wantErr error
	}{
		{
			name: "MissingInteger",
			mutate: func(rule *dbgen.DifficultyRule) {
				rule.ConditionValueInt = pgtype.Int4{}
			},
			wantErr: ErrInvalidBrowserVersionRule,
		},
		{
			name: "Zero",
			mutate: func(rule *dbgen.DifficultyRule) {
				rule.ConditionValueInt.Int32 = 0
			},
			wantErr: ErrInvalidBrowserVersionRule,
		},
		{
			name: "IntegerAndString",
			mutate: func(rule *dbgen.DifficultyRule) {
				rule.ConditionValueStr = pgtype.Text{String: "2", Valid: true}
			},
			wantErr: ErrInvalidBrowserVersionRule,
		},
		{
			name: "Separator",
			mutate: func(rule *dbgen.DifficultyRule) {
				rule.ConditionValueSeparator = pgtype.Text{String: ",", Valid: true}
			},
			wantErr: ErrInvalidBrowserVersionRule,
		},
		{
			name: "WrongOperator",
			mutate: func(rule *dbgen.DifficultyRule) {
				rule.ConditionOperator = dbgen.RuleConditionOperatorEquals
			},
			wantErr: ErrUnsupportedConditionOperator,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := newBrowserVersionRule(2)
			tt.mutate(rule)
			_, err := compiler.CompileRule(t.Context(), rule)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CompileRule error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestBrowserVersionMatchesThresholdAndNegation(t *testing.T) {
	compiler := NewRulesCompiler(useragent.NewParser())
	compiler.BrowserVersions().Replace(map[BrowserVersionKey]int{
		{Browser: BrowserChrome, Platform: PlatformWindows}: 152,
	})
	compiled, err := compiler.CompileRule(t.Context(), newBrowserVersionRule(2))
	if err != nil {
		t.Fatal(err)
	}
	negatedRule := newBrowserVersionRule(2)
	negatedRule.ConditionOperatorNegated = true
	negated, err := compiler.CompileRule(t.Context(), negatedRule)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		major       int
		want        bool
		wantNegated bool
	}{
		{name: "MoreThanThreshold", major: 149, want: true, wantNegated: false},
		{name: "EqualToThreshold", major: 150, want: false, wantNegated: true},
		{name: "Newer", major: 153, want: false, wantNegated: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := browserVersionRequest(browserVersionCases(tt.major)[0].ua)
			if got := compiled.Matches(request); got != tt.want {
				t.Fatalf("Matches() = %v, want %v", got, tt.want)
			}
			if got := negated.Matches(request); got != tt.wantNegated {
				t.Fatalf("negated Matches() = %v, want %v", got, tt.wantNegated)
			}
		})
	}
}

func TestBrowserVersionMatchesSupportedBrowserPlatforms(t *testing.T) {
	for _, tt := range browserVersionCases(149) {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewRulesCompiler(useragent.NewParser())
			compiler.BrowserVersions().Replace(map[BrowserVersionKey]int{tt.key: 152})
			compiled, err := compiler.CompileRule(t.Context(), newBrowserVersionRule(2))
			if err != nil {
				t.Fatal(err)
			}
			if !compiled.Matches(browserVersionRequest(tt.ua)) {
				t.Fatal("supported browser and platform did not match")
			}
		})
	}
}

func TestBrowserVersionCompiledMatcherObservesUpdates(t *testing.T) {
	compiler := NewRulesCompiler(useragent.NewParser())
	key := BrowserVersionKey{Browser: BrowserFirefox, Platform: PlatformLinux}
	compiler.BrowserVersions().Replace(map[BrowserVersionKey]int{key: 152})
	compiled, err := compiler.CompileRule(t.Context(), newBrowserVersionRule(2))
	if err != nil {
		t.Fatal(err)
	}
	request := browserVersionRequest(browserVersionCases(149)[5].ua)
	if !compiled.Matches(request) {
		t.Fatal("initial browser versions did not match")
	}

	compiler.BrowserVersions().Replace(map[BrowserVersionKey]int{key: 151})
	if compiled.Matches(request) {
		t.Fatal("compiled matcher did not observe a non-matching provider update")
	}
}

func TestBrowserVersionFailsOpenForMalformedAndUnsupportedUserAgents(t *testing.T) {
	compiler := NewRulesCompiler(useragent.NewParser())
	compiler.BrowserVersions().Replace(browserVersionsSnapshot(152))
	compiled, err := compiler.CompileRule(t.Context(), newBrowserVersionRule(2))
	if err != nil {
		t.Fatal(err)
	}
	negatedRule := newBrowserVersionRule(2)
	negatedRule.ConditionOperatorNegated = true
	negated, err := compiler.CompileRule(t.Context(), negatedRule)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		ua   string
	}{
		{name: "Empty", ua: ""},
		{name: "SafariMacOS", ua: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"},
		{name: "EdgeWindows", ua: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0"},
		{name: "FirefoxIOS", ua: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/149.0 Mobile/15E148 Safari/605.1.15"},
		{name: "ChromeOS", ua: "Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"},
		{name: "MissingVersion", ua: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/ Safari/537.36"},
		{name: "OverflowingVersion", ua: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/999999999999999999999.0 Safari/537.36"},
		{name: "Oversized", ua: browserVersionCases(149)[0].ua + strings.Repeat("A", browserVersionMaxUserAgentBytes)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := browserVersionRequest(tt.ua)
			if compiled.Matches(request) {
				t.Fatal("malformed or unsupported user agent matched")
			}
			if negated.Matches(request) {
				t.Fatal("malformed or unsupported user agent matched negated rule")
			}
		})
	}
}

func TestBrowserVersionFailsOpenWithoutRuntimeDependencies(t *testing.T) {
	parser := useragent.NewParser()
	versions := NewBrowserVersions()
	versions.Replace(map[BrowserVersionKey]int{
		{Browser: BrowserChrome, Platform: PlatformWindows}: 152,
	})
	request := browserVersionRequest(browserVersionCases(149)[0].ua)
	tests := []struct {
		name    string
		matcher *BrowserVersionMatcher
		stale   bool
	}{
		{name: "NilParser", matcher: &BrowserVersionMatcher{BrowserVersions: versions, Threshold: 2}, stale: true},
		{name: "NilVersions", matcher: &BrowserVersionMatcher{UAParser: parser, Threshold: 2}, stale: true},
		{name: "EmptyVersions", matcher: &BrowserVersionMatcher{UAParser: parser, BrowserVersions: NewBrowserVersions(), Threshold: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.matcher.IsStale(); got != tt.stale {
				t.Fatalf("IsStale() = %v, want %v", got, tt.stale)
			}
			if tt.matcher.Matches(request) {
				t.Fatal("matcher without complete runtime data matched")
			}
		})
	}
	negated := &BrowserVersionMatcher{UAParser: parser, BrowserVersions: NewBrowserVersions(), Threshold: 2, ConditionOperatorNegated: true}
	if negated.Matches(request) {
		t.Fatal("negated matcher without a baseline matched")
	}
}

func TestBrowserVersionGobPreservesNegation(t *testing.T) {
	original := &BrowserVersionMatcher{Threshold: 2, ConditionOperatorNegated: true}
	data, err := original.GobEncode()
	if err != nil {
		t.Fatal(err)
	}

	var decoded BrowserVersionMatcher
	if err := decoded.GobDecode(data); err != nil {
		t.Fatal(err)
	}
	if decoded.Threshold != original.Threshold {
		t.Errorf("Threshold = %d, want %d", decoded.Threshold, original.Threshold)
	}
	if !decoded.ConditionOperatorNegated {
		t.Error("ConditionOperatorNegated = false, want true")
	}
	if !decoded.IsStale() {
		t.Error("decoded matcher is not stale")
	}
	if decoded.Matches(browserVersionRequest(browserVersionCases(149)[0].ua)) {
		t.Error("decoded stale negated matcher matched")
	}
}

func browserVersionsSnapshot(major int) map[BrowserVersionKey]int {
	return map[BrowserVersionKey]int{
		{Browser: BrowserChrome, Platform: PlatformWindows}:  major,
		{Browser: BrowserChrome, Platform: PlatformMacOS}:    major,
		{Browser: BrowserChrome, Platform: PlatformLinux}:    major,
		{Browser: BrowserChrome, Platform: PlatformAndroid}:  major,
		{Browser: BrowserChrome, Platform: PlatformIOS}:      major,
		{Browser: BrowserFirefox, Platform: PlatformWindows}: major,
		{Browser: BrowserFirefox, Platform: PlatformMacOS}:   major,
		{Browser: BrowserFirefox, Platform: PlatformLinux}:   major,
		{Browser: BrowserFirefox, Platform: PlatformAndroid}: major,
	}
}

func TestBrowserVersionsReplacesWholeSnapshot(t *testing.T) {
	provider := NewBrowserVersions()
	key := BrowserVersionKey{Browser: BrowserChrome, Platform: PlatformWindows}
	versions := browserVersionsSnapshot(152)
	provider.Replace(versions)
	versions[key] = 999

	if major, ok := provider.Major(key); !ok || major != 152 {
		t.Fatalf("Major() = %d, %v, want 152, true", major, ok)
	}

	provider.Replace(map[BrowserVersionKey]int{
		{Browser: BrowserFirefox, Platform: PlatformLinux}: 153,
	})
	if _, ok := provider.Major(key); ok {
		t.Fatal("replacement retained an entry from the previous snapshot")
	}
}

func TestBrowserVersionsSupportsConcurrentReadsAndReplacements(t *testing.T) {
	provider := NewBrowserVersions()
	oldSnapshot := browserVersionsSnapshot(152)
	newSnapshot := browserVersionsSnapshot(153)
	provider.Replace(oldSnapshot)
	key := BrowserVersionKey{Browser: BrowserChrome, Platform: PlatformWindows}

	done := make(chan struct{})
	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				major, ok := provider.Major(key)
				if !ok || (major != 152 && major != 153) {
					t.Errorf("concurrent Major() = %d, %v", major, ok)
					return
				}
			}
		}()
	}

	for range 10_000 {
		provider.Replace(newSnapshot)
		provider.Replace(oldSnapshot)
	}
	close(done)
	readers.Wait()
}
