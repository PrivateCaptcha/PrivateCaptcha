//go:build enterprise

package maintenance

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/license"
)

func licenseTestConfig(licenseKey string) common.ConfigStore {
	baseCfg := config.NewBaseConfig(config.NewEnvConfig(os.Getenv))
	baseCfg.Add(config.NewStaticValue(common.ClickHouseOptionalKey, "true"))
	baseCfg.Add(config.NewStaticValue(common.EnterpriseLicenseKeyKey, licenseKey))
	baseCfg.Add(config.NewStaticValue(common.AdminEmailKey, "admin@test.com"))
	return baseCfg
}

func createValidLicense(t *testing.T, keyID int, pubKey ed25519.PublicKey, privKey ed25519.PrivateKey) []byte {
	t.Helper()

	lm := &license.LicenseMessage{
		KeyID:      uint32(keyID),
		UserID:     "test-user-id",
		ProductID:  "test-product-id",
		Expiration: time.Now().UTC().Add(30 * 24 * time.Hour), // 30 days from now
	}

	message, err := lm.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	signature := ed25519.Sign(privKey, message)

	signedMsg := &license.SignedMessage{
		Message:   base64.StdEncoding.EncodeToString(message),
		Signature: base64.StdEncoding.EncodeToString(signature),
	}

	data, err := json.Marshal(signedMsg)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func createExpiredLicense(t *testing.T, keyID int, pubKey ed25519.PublicKey, privKey ed25519.PrivateKey) []byte {
	t.Helper()

	lm := &license.LicenseMessage{
		KeyID:      uint32(keyID),
		UserID:     "test-user-id",
		ProductID:  "test-product-id",
		Expiration: time.Now().UTC().Add(-1 * time.Hour), // Already expired
	}

	message, err := lm.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	signature := ed25519.Sign(privKey, message)

	signedMsg := &license.SignedMessage{
		Message:   base64.StdEncoding.EncodeToString(message),
		Signature: base64.StdEncoding.EncodeToString(signature),
	}

	data, err := json.Marshal(signedMsg)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func TestCheckLicenseJobValidLicense(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = 1

	validLicense := createValidLicense(t, keyID, pubKey, privKey)

	// Create test server that returns valid license
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(validLicense)
	}))
	defer ts.Close()

	// Override LicenseURL for testing
	originalURL := LicenseURL
	LicenseURL = ts.URL
	defer func() { LicenseURL = originalURL }()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, licenseTestConfig("test-license-key"), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	var quitCalled atomic.Bool
	quitFunc := func(ctx context.Context) {
		quitCalled.Store(true)
	}

	keys := []*license.ActivationKey{
		{ID: keyID, Data: pubKey},
	}

	job := &checkLicenseJob{
		store:      store,
		keys:       keys,
		url:        ts.URL,
		licenseKey: config.NewStaticValue(common.EnterpriseLicenseKeyKey, "test-license-key"),
		adminEmail: config.NewStaticValue(common.AdminEmailKey, "admin@test.com"),
		quitFunc:   quitFunc,
		version:    "test-version",
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Errorf("Expected no error with valid license, got: %v", err)
	}

	if quitCalled.Load() {
		t.Error("quit function should not have been called with valid license")
	}

	// Verify job methods
	if job.Name() != "check_license_job" {
		t.Errorf("Expected job name 'check_license_job', got '%s'", job.Name())
	}

	if job.Interval() != 1*time.Hour {
		t.Errorf("Expected interval 1h, got %v", job.Interval())
	}

	if job.Timeout() != 1*time.Minute {
		t.Errorf("Expected timeout 1m, got %v", job.Timeout())
	}

	if job.Jitter() != 10*time.Minute {
		t.Errorf("Expected jitter 10m, got %v", job.Jitter())
	}

	if job.Trigger() != nil {
		t.Error("Expected nil trigger")
	}
}

func TestCheckLicenseJobExpiredLicense(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = 1

	expiredLicense := createExpiredLicense(t, keyID, pubKey, privKey)

	// Create test server that returns expired license
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(expiredLicense)
	}))
	defer ts.Close()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, licenseTestConfig("test-license-key"), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	var quitCalled atomic.Bool
	quitFunc := func(ctx context.Context) {
		quitCalled.Store(true)
	}

	keys := []*license.ActivationKey{
		{ID: keyID, Data: pubKey},
	}

	job := &checkLicenseJob{
		store:      store,
		keys:       keys,
		url:        ts.URL,
		licenseKey: config.NewStaticValue(common.EnterpriseLicenseKeyKey, "test-license-key"),
		adminEmail: config.NewStaticValue(common.AdminEmailKey, "admin@test.com"),
		quitFunc:   quitFunc,
		version:    "test-version",
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err == nil {
		t.Error("Expected error with expired license")
	}

	// Give the goroutine time to execute quit
	time.Sleep(50 * time.Millisecond)

	if !quitCalled.Load() {
		t.Error("quit function should have been called with expired license")
	}
}

func TestCheckLicenseJobInvalidSignature(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Generate two different key pairs
	pubKey1, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, privKey2, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = 1

	// Sign with privKey2 but verify with pubKey1 - should fail
	invalidLicense := createValidLicense(t, keyID, pubKey1, privKey2)

	// Create test server that returns license with invalid signature
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(invalidLicense)
	}))
	defer ts.Close()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, licenseTestConfig("test-license-key"), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	var quitCalled atomic.Bool
	quitFunc := func(ctx context.Context) {
		quitCalled.Store(true)
	}

	keys := []*license.ActivationKey{
		{ID: keyID, Data: pubKey1},
	}

	job := &checkLicenseJob{
		store:      store,
		keys:       keys,
		url:        ts.URL,
		licenseKey: config.NewStaticValue(common.EnterpriseLicenseKeyKey, "test-license-key"),
		adminEmail: config.NewStaticValue(common.AdminEmailKey, "admin@test.com"),
		quitFunc:   quitFunc,
		version:    "test-version",
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err == nil {
		t.Error("Expected error with invalid signature")
	}

	// Give the goroutine time to execute quit
	time.Sleep(50 * time.Millisecond)

	if !quitCalled.Load() {
		t.Error("quit function should have been called with invalid license signature")
	}
}

func TestCheckLicenseJobServerError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Generate test keys
	pubKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = 1

	// Create test server that returns server error
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Bad Request"))
	}))
	defer ts.Close()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, licenseTestConfig("test-license-key"), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	var quitCalled atomic.Bool
	quitFunc := func(ctx context.Context) {
		quitCalled.Store(true)
	}

	keys := []*license.ActivationKey{
		{ID: keyID, Data: pubKey},
	}

	job := &checkLicenseJob{
		store:      store,
		keys:       keys,
		url:        ts.URL,
		licenseKey: config.NewStaticValue(common.EnterpriseLicenseKeyKey, "test-license-key"),
		adminEmail: config.NewStaticValue(common.AdminEmailKey, "admin@test.com"),
		quitFunc:   quitFunc,
		version:    "test-version",
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err == nil {
		t.Error("Expected error when license server returns error")
	}
}

func TestCheckLicenseJobNoLicenseKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Generate test keys
	pubKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = 1

	// Create test server (shouldn't be called)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Server should not have been called without license key")
	}))
	defer ts.Close()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, licenseTestConfig(""), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	var quitCalled atomic.Bool
	quitFunc := func(ctx context.Context) {
		quitCalled.Store(true)
	}

	keys := []*license.ActivationKey{
		{ID: keyID, Data: pubKey},
	}

	job := &checkLicenseJob{
		store:      store,
		keys:       keys,
		url:        ts.URL,
		licenseKey: config.NewStaticValue(common.EnterpriseLicenseKeyKey, ""), // Empty license key
		adminEmail: config.NewStaticValue(common.AdminEmailKey, "admin@test.com"),
		quitFunc:   quitFunc,
		version:    "test-version",
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err == nil {
		t.Error("Expected error when license key is empty")
	}
}

func TestCheckLicenseJobCachedValidLicense(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = 1

	validLicense := createValidLicense(t, keyID, pubKey, privKey)

	// Create test server that should NOT be called (cached license should be used)
	serverCalled := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(validLicense)
	}))
	defer ts.Close()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, licenseTestConfig("test-license-key"), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	// Pre-cache a valid license
	err = store.Impl().StoreInCache(ctx, activationCacheKey, validLicense, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	var quitCalled atomic.Bool
	quitFunc := func(ctx context.Context) {
		quitCalled.Store(true)
	}

	keys := []*license.ActivationKey{
		{ID: keyID, Data: pubKey},
	}

	job := &checkLicenseJob{
		store:      store,
		keys:       keys,
		url:        ts.URL,
		licenseKey: config.NewStaticValue(common.EnterpriseLicenseKeyKey, "test-license-key"),
		adminEmail: config.NewStaticValue(common.AdminEmailKey, "admin@test.com"),
		quitFunc:   quitFunc,
		version:    "test-version",
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Errorf("Expected no error with cached valid license, got: %v", err)
	}

	if quitCalled.Load() {
		t.Error("quit function should not have been called")
	}

	// Server should NOT have been called because cached license has > 7 days until expiration
	if serverCalled {
		t.Error("Server should not have been called when cached license is still valid")
	}
}

func TestGenerateHWID(t *testing.T) {
	t.Parallel()

	// Test that generateHWID returns non-empty results
	salt := "test-salt-123"
	hwid1 := generateHWID(salt)

	if len(hwid1) == 0 {
		t.Error("Expected non-empty HWID")
	}

	// Test that the HWID is a valid hex string (64 chars for SHA256)
	if len(hwid1) != 64 {
		t.Errorf("Expected HWID length of 64, got %d", len(hwid1))
	}

	// Test that different salts produce different HWIDs
	// Note: we cannot test consistency because runtime.ReadMemStats may change between calls
	hwid3 := generateHWID("different-salt")
	if hwid1 == hwid3 {
		t.Error("Expected different HWIDs for different salts")
	}
}

func TestGenerateHWIDWithEmptySalt(t *testing.T) {
	t.Parallel()

	hwid := generateHWID("")

	// Should still generate a valid HWID even with empty salt
	if len(hwid) == 0 {
		t.Error("Expected non-empty HWID even with empty salt")
	}
}

func TestGetMacAddress(t *testing.T) {
	t.Parallel()

	mac, err := getMacAddress()

	// In most environments, we should be able to get a MAC address
	// In Docker, there should be at least a bridge interface
	// If not available, error is expected
	if err != nil {
		if err != errNoMacAddress {
			t.Errorf("Expected errNoMacAddress or success, got %v", err)
		}
		// It's OK if there's no MAC in some test environments
		return
	}

	// If MAC is available, it should be non-empty
	if len(mac) == 0 {
		t.Error("Expected non-empty MAC address")
	}
}

func TestNewCheckLicenseJobEmptyURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Save and restore LicenseURL
	originalURL := LicenseURL
	LicenseURL = ""
	defer func() { LicenseURL = originalURL }()

	pool, _, err := db.Connect(ctx, licenseTestConfig("test-license-key"), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)
	config := licenseTestConfig("test-license-key")

	quitFunc := func(ctx context.Context) {}

	_, err = NewCheckLicenseJob(store, config, "test-version", quitFunc)
	// We expect an error since LicenseURL is empty or because activation keys are not available in test
	if err == nil {
		t.Error("Expected error from NewCheckLicenseJob with empty URL or missing keys")
	}
}
