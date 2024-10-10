package portal

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

const (
	maxSupportFormSizeBytes = 256 * 1024
	supportFormTemplate     = "support/form.html"
	supportTemplate         = "support/support.html"
)

type supportRenderContext struct {
	alertRenderContext
	csrfRenderContext
	Message  string
	Category string
}

func (s *Server) getSupport(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx := &supportRenderContext{
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(user.Email),
		},
	}

	return renderCtx, supportTemplate, nil
}

func categoryFromIndex(ctx context.Context, index string) dbgen.SupportCategory {
	i, err := strconv.Atoi(index)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert support category", "value", index, common.ErrAttr(err))
		return dbgen.SupportCategoryUnknown
	}

	switch i {
	case 0:
		return dbgen.SupportCategoryQuestion
	case 1:
		return dbgen.SupportCategorySuggestion
	case 2:
		return dbgen.SupportCategoryProblem
	default:
		slog.WarnContext(ctx, "Invalid support category index", "index", i)
		return dbgen.SupportCategoryUnknown
	}
}

func (s *Server) postSupport(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()

	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, "", errInvalidRequestArg
	}
	category := categoryFromIndex(ctx, r.FormValue(common.ParamCategory))
	message := strings.TrimSpace(r.FormValue(common.ParamMessage))

	renderCtx := &supportRenderContext{
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(user.Email),
		},
		Message:  message,
		Category: r.FormValue(common.ParamCategory),
	}

	if len(message) < 10 {
		slog.WarnContext(ctx, "Message is too short", "length", len(message))
		renderCtx.ErrorMessage = "Please enter more details."
		return renderCtx, supportFormTemplate, nil
	}

	if err := s.Mailer.SendSupportRequest(ctx, user.Email, string(category), message); err == nil {
		renderCtx.SuccessMessage = "Your message has been sent. We will reply to you soon."
		renderCtx.Message = ""

		_ = s.Store.CreateSupportTicket(ctx, category, message, user.ID)
	} else {
		renderCtx.ErrorMessage = "Failed to send the message. Please try again."
	}

	return renderCtx, supportFormTemplate, nil
}
