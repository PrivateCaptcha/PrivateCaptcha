package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PaddleHQ/paddle-go-sdk"
	"github.com/PaddleHQ/paddle-go-sdk/pkg/paddlenotification"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/rs/xid"
)

const (
	// NOTE: these are real IDs, set in the Paddle Sandbox
	defaultCustomerID     = "ctm_01j03a06d75y3mzm1st230zq6d"
	defaultSubscriptionID = "sub_01j05w0aaj6mrxwpfgnzhkhcq3"
	defaultTransactionID  = "txn_01j05jdt8vpn3n6hwf7djgjdfj"
	defaultPriceID        = "pri_01j03v3ne7q50zfndesjrp74k8"
	defaultProductID      = "pro_01j03v1tmp3gm1mmvhg357cg8v"
)

var ()

func main() {
	stage := os.Getenv("STAGE")
	common.SetupLogs(stage, false /*verbose*/)

	host := os.Getenv("PADDLE_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("PADDLE_PORT")
	if port == "" {
		port = "8080"
	}

	serverBaseURL := os.Getenv("SERVER_BASE_URL")
	if serverBaseURL == "" {
		panic("SERVER_BASE_URL environment variable is not set")
	}
	pcEndpoint := serverBaseURL + common.PaddleSubscriptionCreated

	pcAPIKey := os.Getenv("PC_PRIVATE_API_KEY")

	http.HandleFunc("/"+common.PaddleSubscriptionCreated, func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("Handling request", "path", r.URL.Path, "method", r.Method)
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
			return
		}

		priceID := r.FormValue("price_id")
		if priceID == "" {
			priceID = defaultPriceID
		}

		customerID := r.FormValue("customer_id")
		if customerID == "" {
			customerID = defaultCustomerID
		}

		productID := r.FormValue("product_id")
		if productID == "" {
			productID = defaultProductID
		}

		subscriptionID := r.FormValue("subscription_id")
		if subscriptionID == "" {
			subscriptionID = defaultSubscriptionID
		}

		nextBilledAt := common.JSONTimeNowAdd(7 * 24 * time.Hour).String()

		payload := &paddle.SubscriptionCreatedEvent{
			GenericEvent: paddle.GenericEvent{
				EventID:    xid.New().String(),
				EventType:  "subscription.created",
				OccurredAt: common.JSONTimeNow().String(),
			},
			Data: paddlenotification.SubscriptionCreatedNotification{
				ID:             subscriptionID,
				TransactionID:  defaultTransactionID,
				Status:         paddlenotification.SubscriptionStatus(paddle.SubscriptionStatusTrialing),
				CustomerID:     customerID,
				BusinessID:     nil,
				CurrencyCode:   "EUR",
				CreatedAt:      common.JSONTimeNow().String(),
				UpdatedAt:      common.JSONTimeNow().String(),
				NextBilledAt:   &nextBilledAt,
				CollectionMode: "automatic",
				CurrentBillingPeriod: &paddlenotification.TimePeriod{
					StartsAt: common.JSONTimeNow().String(),
					EndsAt:   common.JSONTimeNowAdd(30 * 24 * time.Hour).String(),
				},
				Items: []paddlenotification.SubscriptionItem{{
					Status:       paddlenotification.SubscriptionItemStatus(paddle.SubscriptionStatusTrialing),
					Quantity:     1,
					Recurring:    true,
					CreatedAt:    common.JSONTimeNow().String(),
					NextBilledAt: &nextBilledAt,
					TrialDates: &paddlenotification.TimePeriod{
						StartsAt: common.JSONTimeNow().String(),
						EndsAt:   common.JSONTimeNowAdd(7 * 24 * time.Hour).String(),
					},
					Price: paddlenotification.Price{ID: priceID, ProductID: productID},
				}},
				CustomData: map[string]any{},
			},
		}

		data, err := json.Marshal(payload)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		slog.Debug("Sending request", "dest", pcEndpoint)
		req, err := http.NewRequest("POST", pcEndpoint, bytes.NewBuffer(data))
		if err != nil {
			http.Error(w, "Failed to create request", http.StatusInternalServerError)
			return
		}

		req.Header.Set(common.HeaderContentType, common.ContentTypeJSON)
		req.Header.Set(common.HeaderAuthorization, "Bearer "+pcAPIKey)
		req.Host = os.Getenv("PC_API_BASE_URL")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Error("Failed to send the HTTP request", common.ErrAttr(err))
			http.Error(w, "Failed to send request to target endpoint", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		// Read the response from the target endpoint
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("Failed to read the response", common.ErrAttr(err))
			http.Error(w, "Failed to read response from target endpoint", http.StatusInternalServerError)
			return
		}
		slog.Debug("Received the response", "code", resp.StatusCode)

		// Write the response back to the client
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	})

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start the server
	server := &http.Server{Addr: net.JoinHostPort(host, port)}
	go func() {
		slog.Debug("Server is listening", "addr", server.Addr)
		if err := server.ListenAndServe(); (err != http.ErrServerClosed) && (err != nil) {
			slog.Info("Failed to start server", common.ErrAttr(err))
		}
	}()

	// Block until a signal is received
	sig := <-sigChan
	slog.Info("Received signal", "code", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.ErrorContext(ctx, "Server forced to shutdown", common.ErrAttr(err))
	}
}
