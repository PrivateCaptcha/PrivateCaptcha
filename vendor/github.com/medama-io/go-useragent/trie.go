package useragent

import (
	"math"
	"slices"
	"unsafe"

	"github.com/medama-io/go-useragent/internal"
)

// trieState is used to determine the current parsing state of the trie.
type trieState uint8

const (
	// stateDefault is the default parsing state of the trie.
	stateDefault trieState = iota
	// stateVersion is the state when we are looking for a version number.
	stateVersion
	// stateSkipWhitespace is the state when we are skipping whitespace.
	stateSkipWhitespace
	// stateSkipClosingParenthesis is the state when we are skipping until a closing parenthesis.
	// This is used to skip over device IDs.
	stateSkipClosingParenthesis
)

type resultItem struct {
	Match internal.Match
	// 0: Unknown, 1: Browser, 2: OS, 3: Type
	Type internal.MatchType
	// Precedence value for each result type to determine which result
	// should be overwritten.
	Precedence uint8
}

type childNode struct {
	node *RuneTrie
	r    rune
}

// RuneTrie is a trie of runes with string keys and interface{} values.
type RuneTrie struct {
	// childrenArr is an array of pointers to the next RuneTrie node. This is used over a map to improve performance
	// and reduce memory usage.
	childrenArr []childNode
	result      []resultItem
}

// Get returns the value stored at the given key. Returns nil for internal
// nodes or for nodes with a value of nil.
func (trie *RuneTrie) Get(key string) UserAgent {
	state := stateDefault
	node := trie
	var ua UserAgent

	// Number of runes to skip when iterating over the trie. This is used
	// to skip over version numbers or language codes.
	var skipCount uint8
	// This is used to determine how many nested parenthesis deep we are.
	var closingParenthisisNestCount uint8

	for i, r := range key {
		if skipCount > 0 {
			skipCount--
			continue
		}

		switch state {
		case stateSkipWhitespace:
			if r == ' ' {
				state = stateDefault
			}

		case stateSkipClosingParenthesis:
			switch r {
			case '(':
				closingParenthisisNestCount++
			case ')':
				if closingParenthisisNestCount == 0 {
					state = stateDefault
				} else {
					closingParenthisisNestCount--
				}
			}

		case stateVersion:
			// In the case of Edg and Edge, skipCount = 1 might just put us on the slash.
			// Ideally, we need to improve the matcher to choose Edge over Edg, but this is
			// a quick fix for now.
			if r == '/' {
				continue
			}

			// If we encounter any unknown characters, we can assume the version number is over.
			if !internal.IsDigit(r) && r != '.' {
				state = stateDefault
			} else {
				// Add to rune buffer.
				if ua.versionIndex < cap(ua.version) {
					ua.version[ua.versionIndex] = r
					ua.versionIndex++
				}
			}

		case stateDefault:
			switch r {
			case ' ', ';', ')', '(', ',', '_', '-', '/':
				continue

			default:
				// Strip any other version numbers from other products to get more hits to the trie.
				//
				// Also do not use a switch here as Go does not generate a jump table for switch
				// statements with no integral constants. Benchmarking shows that ops go down
				// if we try to migrate statements like this to a switch.
				if internal.IsDigit(r) || (r == '.' && len(key) > i+1 && internal.IsDigit(rune(key[i+1]))) {
					continue
				}

				// Identify and skip language codes e.g. en-US, zh-cn, en_US, ZH_cn
				if len(key) > i+6 && r == ' ' && internal.IsLetter(rune(key[i+1])) && internal.IsLetter(rune(key[i+2])) && (key[i+3] == '-' || key[i+3] == '_') && internal.IsLetter(rune(key[i+4])) && internal.IsLetter(rune(key[i+5])) && (key[i+6] == ' ' || key[i+6] == ')' || key[i+6] == ';') {
					// Add the number of runes to skip to the skip count.
					skipCount += 6
					continue
				}
			}

			// If result exists, we can append it to the value.
			for _, result := range node.result {
				matched := ua.addMatch(result)

				// If we matched a browser of the highest precedence, we can mark the
				// next set of runes as the version number we want to store.
				//
				// We also reject any version numbers related to Safari since it has a
				// separate key for its version number.
				if (matched && result.Type == internal.MatchBrowser &&
					result.Match != internal.BrowserSafari) ||
					(result.Type == internal.MatchVersion &&
						ua.versionIndex == 0) {
					// Clear version buffer if it has old values.
					if ua.versionIndex > 0 {
						ua.version = [32]rune{}
						ua.versionIndex = 0
					}

					// We want to omit the slash after the browser name.
					skipCount = 1
					state = stateVersion
				}

				// If we matched a mobile token, we want to strip everything after it
				// until we reach whitespace to get around random device IDs.
				// For example, "Mobile/14F89" should be "Mobile".
				if matched && result.Match == internal.DeviceMobile {
					state = stateSkipWhitespace
				}

				// If we matched an Android token, we want to strip everything after it until
				// we reach a closing parenthesis to get around random device IDs.
				if matched && result.Match == internal.OSAndroid {
					state = stateSkipClosingParenthesis
				}
			}

			// Set the next node to the child of the current node.
			var next *RuneTrie
			if len(node.childrenArr) != 0 {
				for _, child := range node.childrenArr {
					if child.r == r {
						next = child.node
						break
					}
				}
			}

			if next == nil {
				continue // No match found, but we can try to match the next rune.
			}
			node = next
		}
	}

	return ua
}

// Put inserts the value into the trie at the given key, replacing any
// existing items. At the end of key tokens, a result is stored marking
// a potential match for a browser, device, or OS using the indexes provided
// by MatchTokenIndexes.
func (trie *RuneTrie) Put(key string) {
	node := trie
	matchResults := internal.MatchTokenIndexes(key)
	for keyIndex, r := range key {
		// Initialise a new result slice for each new rune.
		if node.result == nil {
			node.result = []resultItem{}
		}

		// If we encounter a match, we can store it in the trie.
		for _, result := range matchResults {
			if keyIndex == result.EndIndex-1 {
				newResult := resultItem{Match: result.Match, Type: result.MatchType, Precedence: result.Precedence}
				if !slices.Contains(node.result, newResult) {
					node.result = append(node.result, newResult)
				}
			}
		}

		var child *RuneTrie

		// Search for the child in the array
		for _, c := range node.childrenArr {
			// If the child is found, set the child to the node.
			if c.r == r {
				child = c.node
				break
			}
		}

		if child == nil {
			// No child found, create a new one
			child = new(RuneTrie)
			node.childrenArr = append(node.childrenArr, childNode{r: r, node: child})
		}

		node = child
	}
}

// This adds a matching constant to a user agent struct.
func (ua *UserAgent) addMatch(result resultItem) bool {
	// Browsers
	if result.Type == internal.MatchBrowser && result.Precedence > ua.browserPrecedence {
		switch result.Match {
		case internal.BrowserChrome,
			internal.BrowserEdge,
			internal.BrowserFirefox,
			internal.BrowserIE,
			internal.BrowserOpera,
			internal.BrowserSafari,
			internal.BrowserVivaldi,
			internal.BrowserSamsung,
			internal.BrowserSilk,
			internal.BrowserFalkon,
			internal.BrowserNintendo,
			internal.BrowserYandex,
			internal.BrowserHuawei:
			ua.browser = result.Match

		case internal.BrowserOperaMini:
			ua.browser = result.Match
			ua.device = internal.DeviceMobile
		}

		ua.browserPrecedence = result.Precedence
		return true
	}

	// Operating Systems
	if result.Type == internal.MatchOS && result.Precedence > ua.osPrecedence {
		switch result.Match {
		case internal.OSChromeOS,
			internal.OSFreeBSD,
			internal.OSOpenBSD,
			internal.OSMacOS,
			internal.OSWindows:
			ua.os = result.Match
			ua.device = internal.DeviceDesktop

		case internal.OSAndroid:
			ua.os = result.Match
			ua.device = internal.DeviceMobile
			// An older generic white-labeled variant of Chrome/Chromium on Android.
			if ua.browser == internal.Unknown {
				ua.browser = internal.BrowserAndroid
				// Special case we set this as the precedence with this is zero
				// and can be overwritten by Safari.
				ua.browserPrecedence = internal.MatchPrecedenceMap[internal.DeviceMobile]
			}

		case internal.OSIOS:
			ua.os = result.Match
			if ua.device != internal.DeviceTablet {
				ua.device = internal.DeviceMobile
			}

		case internal.OSLinux:
			ua.os = result.Match
			if ua.device != internal.DeviceTablet && ua.device != internal.DeviceTV {
				ua.device = internal.DeviceDesktop
			}
		}

		ua.osPrecedence = result.Precedence
		return true
	}

	// Types
	if result.Type == internal.MatchDevice && result.Precedence > ua.typePrecedence {
		switch result.Match {
		case internal.DeviceDesktop,
			internal.DeviceTablet,
			internal.DeviceTV,
			internal.DeviceBot:
			ua.device = result.Match

		case internal.DeviceMobile, internal.TokenMobileDevice:
			if ua.device != internal.DeviceTablet {
				ua.device = internal.DeviceMobile
			}
		}

		ua.typePrecedence = result.Precedence
		return true
	}

	return false
}

// MemoryStats contains memory usage statistics for a trie node
type MemoryStats struct {
	NodeSize        int // Size of this node in bytes
	ChildrenArrSize int // Memory used by children array
	ResultSize      int // Memory used by result slice
	TotalSize       int // Total memory for this node
	ChildrenCount   int // Number of children
	ResultCount     int // Number of result items
}

type ResultStats struct {
	TotalMemoryBytes int64
	NodeCount        int64
	AvgBytesPerNode  float64
	LargestNode      int
	SmallestNode     int
	ArrayNodes       int64
}

// GetMemoryStats returns accurate memory usage statistics for this node.
func (trie *RuneTrie) GetMemoryStats() MemoryStats {
	stats := MemoryStats{}

	// Base struct size
	stats.NodeSize = int(unsafe.Sizeof(*trie))

	// Children array memory
	if len(trie.childrenArr) > 0 {
		stats.ChildrenCount = len(trie.childrenArr)
		// Slice header + capacity * element size
		sliceHeader := int(unsafe.Sizeof([]childNode{}))
		elementSize := int(unsafe.Sizeof(childNode{}))
		stats.ChildrenArrSize = sliceHeader + (cap(trie.childrenArr) * elementSize)
	}

	// Result slice memory
	if len(trie.result) > 0 {
		stats.ResultCount = len(trie.result)
		sliceHeader := int(unsafe.Sizeof([]resultItem{}))
		elementSize := int(unsafe.Sizeof(resultItem{}))
		stats.ResultSize = sliceHeader + (cap(trie.result) * elementSize)
	}

	stats.TotalSize = stats.NodeSize + stats.ChildrenArrSize + stats.ResultSize
	return stats
}

// WalkMemoryStats walks the entire trie and calls the callback for each node
func (trie *RuneTrie) WalkMemoryStats(callback func(stats MemoryStats)) {
	visited := make(map[*RuneTrie]bool)
	trie.walkMemoryStatsRecursive(callback, visited)
}

func (trie *RuneTrie) walkMemoryStatsRecursive(callback func(stats MemoryStats), visited map[*RuneTrie]bool) {
	if trie == nil || visited[trie] {
		return
	}
	visited[trie] = true

	// Call callback for this node
	callback(trie.GetMemoryStats())

	// Walk children in array
	for _, child := range trie.childrenArr {
		if child.node != nil {
			child.node.walkMemoryStatsRecursive(callback, visited)
		}
	}
}

// GetTotalMemoryStats returns aggregate memory statistics for the entire trie
func (trie *RuneTrie) GetTotalMemoryStats() ResultStats {
	result := ResultStats{
		SmallestNode: math.MaxInt, // Start with max int to find the smallest node.
	}

	trie.WalkMemoryStats(func(stats MemoryStats) {
		result.NodeCount++
		result.TotalMemoryBytes += int64(stats.TotalSize)

		if stats.TotalSize > result.LargestNode {
			result.LargestNode = stats.TotalSize
		}
		if stats.TotalSize < result.SmallestNode {
			result.SmallestNode = stats.TotalSize
		}

		result.ArrayNodes++
	})

	if result.NodeCount > 0 {
		result.AvgBytesPerNode = float64(result.TotalMemoryBytes) / float64(result.NodeCount)
	}

	return result
}

// NewRuneTrie allocates and returns a new *RuneTrie.
func NewRuneTrie() *RuneTrie {
	return new(RuneTrie)
}
