package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

func viewwidgetPuzzle(t *testing.T, target string) (*server, *puzzle.ComputePuzzle, string, int) {
	t.Helper()
	srv := &server{salt: puzzle.NewSalt([]byte("test-salt"))}
	router := http.NewServeMux()
	srv.Setup(router)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	if w.Code != http.StatusOK {
		return srv, nil, w.Body.String(), w.Code
	}
	encoded, _, _ := strings.Cut(w.Body.String(), ".")
	body, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	p := &puzzle.ComputePuzzle{}
	if err := p.UnmarshalBinary(body); err != nil {
		t.Fatal(err)
	}
	return srv, p, w.Body.String(), w.Code
}

func TestViewwidgetPuzzle(t *testing.T) {
	for _, tc := range []struct {
		name      string
		target    string
		challenge puzzle.Challenge
		wire      uint8
		count     int
	}{
		{name: "Default", target: "/puzzle", challenge: puzzle.ChallengeArgon2ID, wire: 40, count: puzzle.Argon2IDSolutionsCount},
		{name: "ArgonMinimum", target: "/puzzle/152", challenge: puzzle.ChallengeArgon2ID, wire: 24, count: puzzle.Argon2IDSolutionsCount},
		{name: "Cutover", target: "/puzzle/151", challenge: puzzle.ChallengeBlake2b, wire: 151, count: 24},
		{name: "ZeroWireFallback", target: "/puzzle/128", challenge: puzzle.ChallengeBlake2b, wire: 128, count: 24},
		{name: "BlakeOverride", target: "/puzzle/152?challenge=blake2b", challenge: puzzle.ChallengeBlake2b, wire: 152, count: 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, issued, _, code := viewwidgetPuzzle(t, tc.target)
			if code != http.StatusOK || issued.PuzzleID() == 0 || issued.Challenge() != tc.challenge ||
				issued.Difficulty() != tc.wire || issued.SolutionsCount() != tc.count {
				t.Fatalf("puzzle = %+v, status = %d; want challenge %d wire %d count %d and nonzero ID", issued, code, tc.challenge, tc.wire, tc.count)
			}
		})
	}
	_, _, _, code := viewwidgetPuzzle(t, "/puzzle?challenge=unknown")
	if code != http.StatusBadRequest {
		t.Fatalf("unknown challenge status = %d, want %d", code, http.StatusBadRequest)
	}
	_, echo, _, code := viewwidgetPuzzle(t, "/echopuzzle")
	if code != http.StatusOK || echo.Challenge() != puzzle.ChallengeBlake2b || echo.PuzzleID() != 0 {
		t.Fatalf("echo puzzle = %+v, status = %d", echo, code)
	}
}

func TestViewwidgetSubmit(t *testing.T) {
	srv, issued, encoded, code := viewwidgetPuzzle(t, "/puzzle/152")
	if code != http.StatusOK || issued.PuzzleID() == 0 {
		t.Fatalf("uninitialized puzzle = %+v, status = %d", issued, code)
	}
	solutions, err := (&puzzle.ComputeSolver{}).Solve(t.Context(), issued)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(payload string) string {
		t.Helper()
		form := url.Values{"private-captcha-solution": {payload}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router := http.NewServeMux()
		srv.Setup(router)
		router.ServeHTTP(w, req)
		return w.Body.String()
	}
	if result := verify(solutions.String() + "." + encoded); !strings.Contains(result, "background-color: green") {
		t.Fatalf("valid Argon work did not verify: %s", result)
	} else if !strings.Contains(result, `href="/assets/img/favicon.png"`) {
		t.Fatalf("success page does not use the viewer favicon: %s", result)
	}
	parts := strings.Split(encoded, ".")
	signature, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	signature[len(signature)-1] ^= 1
	if result := verify(solutions.String() + "." + parts[0] + "." + base64.StdEncoding.EncodeToString(signature)); !strings.Contains(result, "background-color: red") {
		t.Fatalf("invalid signature bypassed verification: %s", result)
	} else if !strings.Contains(result, `href="/assets/img/favicon.png"`) {
		t.Fatalf("failure page does not use the viewer favicon: %s", result)
	}
}

func TestViewwidgetMainPageChallenge(t *testing.T) {
	for _, tc := range []struct {
		target   string
		endpoint string
	}{
		{target: "/", endpoint: `data-puzzle-endpoint="/puzzle"`},
		{target: "/?challenge=blake2b", endpoint: `data-puzzle-endpoint="/puzzle?challenge=blake2b"`},
		{target: "/?level=136&challenge=blake2b", endpoint: `data-puzzle-endpoint="/puzzle/136?challenge=blake2b"`},
		{target: "/?echo=true&challenge=blake2b", endpoint: `data-puzzle-endpoint="/echopuzzle"`},
		{target: "/?echo=true&level=136&challenge=blake2b", endpoint: `data-puzzle-endpoint="/echopuzzle"`},
	} {
		t.Run(tc.target, func(t *testing.T) {
			w := httptest.NewRecorder()
			staticHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tc.endpoint) {
				t.Fatalf("page status = %d, endpoint %q missing", w.Code, tc.endpoint)
			}
			if !strings.Contains(w.Body.String(), `src="/widget/js/privatecaptcha-ext.js`) {
				t.Fatal("main page does not load the extended widget")
			}
		})
	}
	w := httptest.NewRecorder()
	staticHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/?challenge=unknown", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown page challenge status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestViewwidgetEchoSubmit(t *testing.T) {
	srv, issued, encoded, code := viewwidgetPuzzle(t, "/echopuzzle")
	if code != http.StatusOK || issued.Challenge() != puzzle.ChallengeBlake2b || !issued.IsStub() {
		t.Fatalf("echo puzzle = %+v, status = %d", issued, code)
	}
	solutions := &puzzle.Solutions{Buffer: make([]byte, issued.SolutionsCount()*puzzle.SolutionLength), Metadata: &puzzle.Metadata{}}
	form := url.Values{"private-captcha-solution": {solutions.String() + "." + encoded}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router := http.NewServeMux()
	srv.Setup(router)
	router.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "background-color: green") {
		t.Fatalf("echo submit did not succeed: %s", w.Body.String())
	}
}

func TestViewwidgetPageParity(t *testing.T) {
	for _, tc := range []struct {
		target   string
		endpoint string
	}{
		{target: "/popup.html", endpoint: `data-puzzle-endpoint="/puzzle"`},
		{target: "/dark.html", endpoint: `data-puzzle-endpoint="/puzzle"`},
		{target: "/popup.html?level=136&challenge=blake2b", endpoint: `data-puzzle-endpoint="/puzzle/136?challenge=blake2b"`},
		{target: "/dark.html?level=152&challenge=blake2b", endpoint: `data-puzzle-endpoint="/puzzle/152?challenge=blake2b"`},
		{target: "/popup.html?echo=true&challenge=blake2b", endpoint: `data-puzzle-endpoint="/echopuzzle"`},
		{target: "/dark.html?echo=true&challenge=blake2b", endpoint: `data-puzzle-endpoint="/echopuzzle"`},
		{target: "/popup.html?echo=true&level=136&challenge=blake2b", endpoint: `data-puzzle-endpoint="/echopuzzle"`},
		{target: "/dark.html?echo=true&level=136&challenge=blake2b", endpoint: `data-puzzle-endpoint="/echopuzzle"`},
	} {
		t.Run(tc.target, func(t *testing.T) {
			w := httptest.NewRecorder()
			staticHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tc.endpoint) {
				t.Fatalf("page status = %d, endpoint %q missing", w.Code, tc.endpoint)
			}
			if !strings.Contains(w.Body.String(), `src="/widget/js/privatecaptcha-ext.js"`) {
				t.Fatal("viewer page does not load the extended widget")
			}
			if strings.HasPrefix(tc.target, "/dark.html") && !strings.Contains(w.Body.String(), `action="/submit"`) {
				t.Fatal("dark page must submit to the real verification endpoint")
			}
			if strings.HasPrefix(tc.target, "/dark.html") && !strings.Contains(w.Body.String(), `src="/assets/img/pc-icon-light.svg"`) {
				t.Fatal("dark page must use the local icon")
			}
		})
	}
}

func TestViewwidgetStylesheetVersion(t *testing.T) {
	for _, target := range []string{"/", "/popup.html", "/dark.html"} {
		w := httptest.NewRecorder()
		staticHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/assets/css/style.css?v="+web.AssetVersion()) {
			t.Fatalf("%s does not use versioned stylesheet", target)
		}
	}
}
