package tests

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

type StubPuzzleEngine struct {
	Result *puzzle.VerifyResult
}

func (f *StubPuzzleEngine) ParseSolutionPayload(ctx context.Context, data []byte) (puzzle.SolutionPayload, error) {
	return puzzle.NewStubPayload(puzzle.NewComputePuzzle(0, [puzzle.PropertyIDSize]byte{}, 0)), nil
}

func (f *StubPuzzleEngine) Create(puzzleID uint64, propertyID [puzzle.PropertyIDSize]byte, difficulty uint8) puzzle.Puzzle {
	return puzzle.NewComputePuzzle(puzzleID, propertyID, difficulty)
}

func (f *StubPuzzleEngine) Write(ctx context.Context, p puzzle.Puzzle, extraSalt []byte, w http.ResponseWriter) error {
	return nil
}

func (f *StubPuzzleEngine) Verify(ctx context.Context, payload puzzle.SolutionPayload, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (*puzzle.VerifyResult, error) {
	return f.Result, nil
}

func wrapScriptContentsWithCDATA(input []byte) []byte {
	re := regexp.MustCompile(`(?s)(<script[^>]*>)(.*?)(</script>)`)

	return re.ReplaceAllFunc(input, func(match []byte) []byte {
		parts := re.FindSubmatch(match)
		if len(parts) != 4 {
			return match // safety fallback
		}
		openTag := parts[1]
		content := parts[2]
		closeTag := parts[3]

		// Skip if already wrapped in CDATA
		if bytes.Contains(content, []byte("<![CDATA[")) {
			return match
		}

		var buf bytes.Buffer
		buf.Write(openTag)
		buf.WriteString("<![CDATA[")
		buf.Write(content)
		buf.WriteString("]]>")
		buf.Write(closeTag)
		return buf.Bytes()
	})
}

// courtesy of https://martinfowler.com/articles/tdd-html-templates.html
func AssertWellFormedHTML(t *testing.T, buf *bytes.Buffer) {
	data := buf.Bytes()
	// '<=' (e.g. in for loops) in <script> breaks XML parser
	data = wrapScriptContentsWithCDATA(data)
	// special handling for Alpine.js, otherwise we get XML parsing error "attribute expected"
	data = bytes.ReplaceAll(data, []byte(" @click="), []byte(" click="))
	data = bytes.ReplaceAll(data, []byte(" @click."), []byte(" click."))
	data = bytes.ReplaceAll(data, []byte(" @htmx:"), []byte(" htmx-"))
	data = bytes.ReplaceAll(data, []byte(" hx-on::"), []byte(" hx-on-"))

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity
	for {
		token, err := decoder.Token()
		switch err {
		case io.EOF:
			return // We're done, it's valid!
		case nil:
			// do nothing
		default:
			for i, line := range bytes.Split(data, []byte("\n")) {
				t.Logf("%d: %s\n", i+1, line)
			}
			t.Fatalf("Error parsing html: %s, %v", err, token)
		}
	}
}

func ParseHTML(t *testing.T, buf *bytes.Buffer) *goquery.Document {
	AssertWellFormedHTML(t, buf)
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		// if parsing fails, we stop the test here with t.FatalF
		t.Fatalf("Error rendering template %s", err)
	}
	return document
}

func Text(node *html.Node) string {
	// A little mess due to the fact that goquery has
	// a .Text() method on Selection but not on html.Node
	sel := goquery.Selection{Nodes: []*html.Node{node}}
	return strings.TrimSpace(sel.Text())
}

func TwoFactorCodeFromResponse(ctx context.Context, resp *http.Response, sessions *session.Manager) (int, error) {
	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == sessions.CookieName })
	if idx == -1 {
		return 0, errors.New("failed to find a cookie " + sessions.CookieName)
	}
	cookie := resp.Cookies()[idx]

	return TwoFactorCodeFromSession(ctx, cookie.Value, sessions.Store)
}

func TwoFactorCodeFromSession(ctx context.Context, cookie string, store session.Store) (int, error) {
	sess, err := store.Read(ctx, cookie, false /*skip cache*/)
	if err != nil {
		return 0, err
	}

	if code, ok := sess.Get(ctx, session.KeyTwoFactorCode).(int); ok {
		return code, nil
	}

	return 0, errors.New("2FA code not found in session")
}

func AuthenticateSuite(ctx context.Context, email string, srv *http.ServeMux, xsrf *common.XSRFMiddleware, sessions *session.Manager) (*http.Cookie, error) {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, xsrf.Token(""))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamPortalSolution, "captchaSolution")

	// Send the POST request
	req := httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == sessions.CookieName })
	if idx == -1 {
		return nil, errors.New("cannot find session cookie in response")
	}
	cookie := resp.Cookies()[idx]

	code, err := TwoFactorCodeFromSession(ctx, cookie.Value, sessions.Store)
	if err != nil {
		return cookie, err
	}

	form = url.Values{}
	form.Add(common.ParamCSRFToken, xsrf.Token(email))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamVerificationCode, strconv.Itoa(code))

	// now send the 2fa request
	req = httptest.NewRequest("POST", "/"+common.TwoFactorEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()

	if resp.StatusCode != http.StatusSeeOther {
		return nil, fmt.Errorf("unexpected post twofactor code: %v", resp.StatusCode)
	}

	if location, _ := resp.Location(); location.String() != "/" {
		return nil, fmt.Errorf("unexpected redirect: %v", location)
	}

	slog.Log(ctx, common.LevelTrace, "Looks like we are authenticated", "code", resp.StatusCode)

	return cookie, nil
}
