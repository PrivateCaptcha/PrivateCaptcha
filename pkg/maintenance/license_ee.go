//go:build enterprise

package maintenance

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/license"
	"github.com/denisbrodbeck/machineid"
	"github.com/jpillora/backoff"
	"github.com/rs/xid"
)

const (
	activationCacheKey    = "license_activation"
	activationAPIAttempts = 8
)

var (
	defaultLicenseURL = fmt.Sprintf("https://api.privatecaptcha.com/%s/%s", common.SelfHostedEndpoint, common.ActivationEndpoint)
	// NOTE: for testing, replace with host.docker.internal domain
	LicenseURL               string
	errLicenseRequest        = errors.New("license request error")
	errLicenseServer         = errors.New("license server error")
	errEnterpriseConfigError = errors.New("enterprise config error")
	errNoEnterpriseKeys      = errors.New("enterprise keys not found")
	errNoMacAddress          = errors.New("mac address not found")
)

func init() {
	if len(LicenseURL) == 0 {
		LicenseURL = defaultLicenseURL
	}
}

func NewCheckLicenseJob(store db.Implementor, config common.ConfigStore, version string, quitFunc func(ctx context.Context)) (common.PeriodicJob, error) {
	keys, err := license.ActivationKeys()
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return nil, errNoEnterpriseKeys
	}

	if len(LicenseURL) == 0 {
		return nil, errEnterpriseConfigError
	}

	return &checkLicenseJob{
		store:      store,
		keys:       keys,
		url:        LicenseURL,
		licenseKey: config.Get(common.EnterpriseLicenseKeyKey),
		adminEmail: config.Get(common.AdminEmailKey),
		quitFunc:   quitFunc,
		version:    version,
	}, nil
}

type checkLicenseJob struct {
	store      db.Implementor
	keys       []*license.ActivationKey
	url        string
	licenseKey common.ConfigItem
	adminEmail common.ConfigItem
	quitFunc   func(ctx context.Context)
	version    string
}

var _ common.PeriodicJob = (*checkLicenseJob)(nil)

func doFetchActivation(ctx context.Context, licenseURL, licenseKey, hwid, version string) ([]byte, error) {
	form := url.Values{}
	form.Set(common.ParamLicenseKey, licenseKey)
	form.Set(common.ParamHardwareID, hwid)
	form.Set(common.ParamVersion, version)

	req, err := http.NewRequest(http.MethodPost, licenseURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, err
	}

	rid := xid.New().String()
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set(common.HeaderTraceID, rid)

	rlog := slog.With("requestID", rid)
	rlog.DebugContext(ctx, "Sending license request", "URL", licenseURL)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, common.NewRetriableError(err)
	}
	defer resp.Body.Close()

	const maxBytes = 256 * 1024
	limitedReader := io.LimitReader(resp.Body, maxBytes)
	responseData, responseErr := io.ReadAll(limitedReader)

	// we _can_ retry on server errors and few select client errors (e.g. rate limiting)
	if (resp.StatusCode >= 500) ||
		(resp.StatusCode == http.StatusTooManyRequests) ||
		(resp.StatusCode == http.StatusRequestTimeout) ||
		(resp.StatusCode == http.StatusTooEarly) {
		rlog.WarnContext(ctx, "Failed to fetch activation", "code", resp.StatusCode, "response", string(responseData))
		return nil, common.NewRetriableError(errLicenseServer)
	}

	// the difference is that we don't retry on most 4xx (client) errors (e.g. BadRequest / Forbidden)
	if resp.StatusCode >= 400 {
		rlog.WarnContext(ctx, "Failed to fetch activation", "code", resp.StatusCode, "response", string(responseData))

		return nil, errLicenseRequest
	}

	rlog.DebugContext(ctx, "Received license response", "code", resp.StatusCode, "response", len(responseData))

	return responseData, responseErr
}

func getMacAddress() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range interfaces {
		if (iface.Flags&net.FlagUp != 0) && (iface.Flags&net.FlagLoopback == 0) {
			if str := iface.HardwareAddr.String(); len(str) > 0 {
				return str, nil
			}
		}
	}

	return "", errNoMacAddress
}

func generateHWID(salt string) string {
	hasher := hmac.New(sha256.New, []byte(salt))

	if mac, err := getMacAddress(); err == nil {
		_, _ = hasher.Write([]byte(mac))
	}

	if hostname, err := os.Hostname(); err == nil {
		_, _ = hasher.Write([]byte(hostname))
	}

	numCPU := runtime.NumCPU()
	_ = binary.Write(hasher, binary.LittleEndian, int32(numCPU))

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	_ = binary.Write(hasher, binary.LittleEndian, m.Sys)

	return hex.EncodeToString(hasher.Sum(nil))
}

func (j *checkLicenseJob) fetchActivation(ctx context.Context) ([]byte, error) {
	licenseKey := j.licenseKey.Value()
	if len(licenseKey) == 0 {
		return nil, errEnterpriseConfigError
	}

	const app = "PrivateCaptcha"
	hwid, merr := machineid.ProtectedID(app)
	if merr != nil {
		slog.ErrorContext(ctx, "Failed to generate HWID", common.ErrAttr(merr))
		hwid = generateHWID(app)
	}

	b := &backoff.Backoff{
		Min:    1 * time.Second,
		Max:    10 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	var data []byte
	var err error

	for i := 0; i < activationAPIAttempts; i++ {
		if i > 0 {
			time.Sleep(b.Duration())
		}

		data, err = doFetchActivation(ctx, j.url, licenseKey, hwid, j.version)

		var rerr common.RetriableError
		if err != nil && errors.As(err, &rerr) {
			err = rerr.Unwrap()
			slog.WarnContext(ctx, "Failed to fetch activation", "attempt", i, common.ErrAttr(err))
		} else {
			break
		}
	}

	return data, err
}

func (j *checkLicenseJob) activateLicense(ctx context.Context, tnow time.Time) error {
	data, err := j.fetchActivation(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch activation", common.ErrAttr(err))
		return err
	}

	msg, err := license.VerifyActivation(ctx, data, j.keys, tnow)
	if err == nil {
		slog.InfoContext(ctx, "Server activation is valid")
		_ = j.store.Impl().StoreInCache(ctx, activationCacheKey, data, msg.Expiration.Sub(tnow))
	} else {
		slog.ErrorContext(ctx, "Failed to verify server activation", common.ErrAttr(err))
	}

	return err
}

func (j *checkLicenseJob) checkLicense(ctx context.Context) error {
	if len(j.keys) == 0 {
		slog.ErrorContext(ctx, "No license keys available")
		return errEnterpriseConfigError
	}

	tnow := time.Now().UTC()
	cachedIsValid := false

	if data, err := j.store.Impl().RetrieveFromCache(ctx, activationCacheKey); err == nil {
		if msg, err := license.VerifyActivation(ctx, data, j.keys, tnow); err == nil {
			cachedIsValid = true
			expiration := msg.Expiration.Sub(tnow)
			slog.InfoContext(ctx, "Cached activation is valid", "expiration", expiration.String())
			if expiration.Hours() > 24*7 {
				return nil
			}
			// else we will proceed below to actually fetch it again
		} else {
			slog.WarnContext(ctx, "Failed to verify cached activation", common.ErrAttr(err))
		}
	} else {
		slog.WarnContext(ctx, "Activation is not cached", common.ErrAttr(err))
	}

	if err := j.activateLicense(ctx, tnow); err != nil {
		if cachedIsValid {
			// create warning, but swallow the error
			adminEmail := j.adminEmail.Value()
			if admin, aerr := j.store.Impl().FindUserByEmail(ctx, adminEmail); aerr == nil {
				// truncating time will cause duplicate notification being rejected based on SQL constraint
				notifTime := tnow.Truncate(24 * time.Hour)
				notifDuration := 7 * 24 * time.Hour
				text := fmt.Sprintf("Failed to renew EE license (%s): <i>%s</i>", tnow.Format(time.DateOnly), err.Error())
				_, _ = j.store.Impl().CreateSystemNotification(ctx, text, notifTime, &notifDuration, &admin.ID)
			} else {
				slog.ErrorContext(ctx, "Failed to find admin user by email", "email", adminEmail, common.ErrAttr(aerr))
			}

			return nil
		}

		return err
	}

	return nil
}

func (j *checkLicenseJob) RunOnce(ctx context.Context, params any) error {
	if err := j.checkLicense(ctx); err != nil {
		go j.quitFunc(ctx)
		return err
	}

	return nil
}

func (j *checkLicenseJob) NewParams() any {
	return struct{}{}
}

func (j *checkLicenseJob) Interval() time.Duration {
	// we run frequently intentionally (for license verification) but we should hit cache 99.9% of the time
	return 1 * time.Hour
}

func (j *checkLicenseJob) Jitter() time.Duration {
	return 10 * time.Minute
}

func (j *checkLicenseJob) Name() string {
	return "check_license_job"
}
