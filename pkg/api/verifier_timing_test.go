package api

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

func TestMinSolveTimeAtMediumUpdate(t *testing.T) {
	p := puzzle.NewComputePuzzle(123, [puzzle.PropertyIDSize]byte{}, 168)
	solutions, err := (&puzzle.ComputeSolver{}).Solve(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := solutions.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(raw[3:7], 1)
	signed, err := p.Serialize(t.Context(), puzzle.NewSalt([]byte("test-salt")), nil)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	encoded.WriteString(base64.StdEncoding.EncodeToString(raw))
	encoded.WriteByte('.')
	if err := signed.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	payload, err := puzzle.ParseVerifyPayload[puzzle.ComputePuzzle](t.Context(), encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	value := "500us"
	env := config.NewEnvConfig(func(name string) string {
		if name == "PC_MIN_SOLVE_TIME_AT_MEDIUM" {
			return value
		}
		return ""
	})
	cfg := config.NewBaseConfig(testsConfigStore())
	item := env.Get(common.MinSolveTimeAtMediumKey)
	cfg.Add(item)
	verifier := NewVerifier(cfg, nil, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	if verifier.MinSolveTimeAtMediumKey != item {
		t.Fatal("verifier did not retain its minimum solve time config item")
	}
	if _, result, err := verifier.verifyPayload(t.Context(), payload, nil, false); err != nil || result != puzzle.VerifyNoError {
		t.Fatalf("initial minimum: result = %v, err = %v", result, err)
	}
	for _, tc := range []struct {
		value string
		want  puzzle.VerifyError
	}{
		{
			value: "500us",
			want:  puzzle.VerifyNoError,
		},
		{
			value: "1.5ms",
			want:  puzzle.SolutionTimingError,
		},
		{
			value: "1ms",
			want:  puzzle.VerifyNoError,
		},
		{
			value: "invalid",
			want:  puzzle.VerifyNoError,
		},
		{
			value: "",
			want:  puzzle.VerifyNoError,
		},
	} {
		value = tc.value
		env.Update(t.Context())
		if err := verifier.Update(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, result, err := verifier.verifyPayload(t.Context(), payload, nil, false); err != nil || result != tc.want {
			t.Fatalf("minimum %q: result = %v, err = %v, want %v", value, result, err, tc.want)
		}
	}
	verifier.MinSolveTimeAtMediumKey = nil
	verifier.updateMinSolveTimeAtMedium(t.Context())
	if _, result, err := verifier.verifyPayload(t.Context(), payload, nil, false); err != nil || result != puzzle.VerifyNoError {
		t.Fatalf("nil timing config: result = %v, err = %v", result, err)
	}
}

func TestVerifySolutionTiming(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	previous := server.Verifier.MinSolveTimeAtMediumKey
	server.Verifier.MinSolveTimeAtMediumKey = config.NewStaticValue(common.MinSolveTimeAtMediumKey, "100ms")
	server.Update(t.Context())
	t.Cleanup(func() {
		server.Verifier.MinSolveTimeAtMediumKey = previous
		server.Update(t.Context())
	})
	for _, compat := range []bool{false, true} {
		for _, elapsed := range []uint32{0, 1, 3600000, ^uint32(0)} {
			t.Run(fmt.Sprintf("Compat%tMillis%d", compat, elapsed), func(t *testing.T) {
				payload, secret, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
				if err != nil {
					t.Fatal(err)
				}
				parts := strings.SplitN(payload, ".", 2)
				solutions, err := base64.StdEncoding.DecodeString(parts[0])
				if err != nil {
					t.Fatal(err)
				}
				binary.LittleEndian.PutUint32(solutions[3:7], elapsed)
				payload = base64.StdEncoding.EncodeToString(solutions) + "." + parts[1]

				for _, expected := range []puzzle.VerifyError{puzzle.SolutionTimingError, puzzle.VerifiedBeforeError} {
					var response *http.Response
					if compat {
						response, err = siteVerifySuite(payload, secret, sitekey)
					} else {
						response, err = verifySuite(payload, secret, sitekey)
					}
					if err != nil {
						t.Fatal(err)
					}
					defer response.Body.Close()
					if response.StatusCode != http.StatusOK {
						t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
					}
					wantSuccess := expected == puzzle.SolutionTimingError
					if compat {
						var result VerifyResponseRecaptchaV2
						if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
							t.Fatal(err)
						}
						if result.Success != wantSuccess || len(result.ErrorCodes) != 1 || result.ErrorCodes[0] != expected.String() {
							t.Fatalf("siteverify response = %+v, want success = %t, code = %v", result, wantSuccess, expected)
						}
					} else {
						var result VerificationResponse
						if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
							t.Fatal(err)
						}
						if result.Success != wantSuccess || result.Code != expected {
							t.Fatalf("verify response = %+v, want success = %t, code = %v", result, wantSuccess, expected)
						}
						if wantSuccess && (result.Timestamp == nil || result.Origin == "") {
							t.Fatalf("timing diagnostic lost puzzle context: %+v", result)
						}
					}
				}
			})
		}
	}
}

func TestVerifyMinSolveTimeAtMediumReload(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	previous := server.Verifier.MinSolveTimeAtMediumKey
	t.Cleanup(func() {
		server.Verifier.MinSolveTimeAtMediumKey = previous
		server.Update(t.Context())
	})
	for _, tc := range []struct {
		value string
		want  puzzle.VerifyError
	}{
		{
			value: "100ms",
			want:  puzzle.SolutionTimingError,
		},
		{
			value: "10ms",
			want:  puzzle.VerifyNoError,
		},
		{
			value: "",
			want:  puzzle.VerifyNoError,
		},
		{
			value: "invalid",
			want:  puzzle.VerifyNoError,
		},
	} {
		t.Run(tc.value, func(t *testing.T) {
			server.Verifier.MinSolveTimeAtMediumKey = config.NewStaticValue(common.MinSolveTimeAtMediumKey, tc.value)
			server.Update(t.Context())
			payload, secret, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
			if err != nil {
				t.Fatal(err)
			}
			parts := strings.SplitN(payload, ".", 2)
			solutions, err := base64.StdEncoding.DecodeString(parts[0])
			if err != nil {
				t.Fatal(err)
			}
			binary.LittleEndian.PutUint32(solutions[3:7], 30)
			payload = base64.StdEncoding.EncodeToString(solutions) + "." + parts[1]
			response, err := verifySuite(payload, secret, sitekey)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var result VerificationResponse
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK || !result.Success || result.Code != tc.want {
				t.Fatalf("minimum %q: status = %d, result = %+v, want %v", tc.value, response.StatusCode, result, tc.want)
			}
		})
	}
}
