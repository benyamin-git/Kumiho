package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
)

func TestLoginUnblockCodeFlow(t *testing.T) {
	var loginCalls, sendCalls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode login body: %v", err)
				return
			}
			if loginCalls.Add(1) == 1 {
				if _, ok := body["unblockCode"]; ok {
					t.Errorf("first login must not send an unblockCode: %#v", body)
				}
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"errno":125,"message":"locked","verificationMethod":"email-2fa","verificationReason":"unblock-code"}`)
				return
			}
			if body["unblockCode"] != "ABCD-1234" {
				t.Errorf("unblockCode = %v", body["unblockCode"])
			}
			if _, ok := body["verificationMethod"]; ok {
				t.Errorf("unblock login must omit verificationMethod: %#v", body)
			}
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionHex())
		case "/v1/account/login/send_unblock_code":
			sendCalls.Add(1)
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode unblock body: %v", err)
			}
			if body["email"] != "user@example.com" {
				t.Errorf("unblock email = %v", body["email"])
			}
			fmt.Fprint(w, `{}`)
		case "/v1/oauth/token":
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	if ls := ctrl.LoginEmail("user@example.com"); ls.Step != "password" || ls.Method != "email" {
		t.Fatalf("login_email = %+v", ls)
	}
	ls := ctrl.LoginPassword(ctx, "pw")
	if ls.Step != "2fa" || ls.Method != "unblock" || ls.Error != "" {
		t.Fatalf("login_password = %+v", ls)
	}
	if ls.VerificationMethod != "email-2fa" || ls.VerificationReason != "unblock-code" {
		t.Fatalf("verification fields = %+v", ls)
	}
	if got := sendCalls.Load(); got != 1 {
		t.Fatalf("send_unblock_code calls = %d, want 1", got)
	}

	ls = ctrl.Login2FA(ctx, api.Login2FAPayload{Code: "ABCD-1234"})
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("login_2fa = %+v", ls)
	}
	if got := loginCalls.Load(); got != 2 {
		t.Fatalf("login calls = %d, want 2", got)
	}
	if st := ctrl.Snapshot(); !st.Authenticated {
		t.Fatalf("not authenticated after unblock login: %+v", st)
	}
}

func TestLoginTOTPFlow(t *testing.T) {
	var totpCalls, verifyCodeCalls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":false,"verificationMethod":"totp-2fa"}`, testSessionHex())
		case "/v1/session/verify/totp":
			totpCalls.Add(1)
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode totp body: %v", err)
			}
			if body["code"] != "123456" {
				t.Errorf("totp code = %v", body["code"])
			}
			fmt.Fprint(w, `{}`)
		case "/v1/session/verify_code":
			verifyCodeCalls.Add(1)
			http.NotFound(w, r)
		case "/v1/oauth/token":
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	ls := ctrl.LoginPassword(ctx, "pw")
	if ls.Step != "2fa" || ls.Method != "totp" || ls.VerificationMethod != "totp-2fa" || ls.Error != "" {
		t.Fatalf("login_password = %+v", ls)
	}
	ls = ctrl.Login2FA(ctx, api.Login2FAPayload{Code: "123456"})
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("login_2fa = %+v", ls)
	}
	if totpCalls.Load() != 1 || verifyCodeCalls.Load() != 0 {
		t.Fatalf("totp calls = %d, verify_code calls = %d; want 1 and 0", totpCalls.Load(), verifyCodeCalls.Load())
	}
	if st := ctrl.Snapshot(); !st.Authenticated {
		t.Fatalf("not authenticated after TOTP login: %+v", st)
	}
}

// A 2FA code submit challenged by an image CAPTCHA must pause for a person.
// v1.1.0 only installed the solver on the password request, so this call
// returned the code screen with "could not solve the anti-bot challenge".
func TestLogin2FACaptchaOnVerify(t *testing.T) {
	const prefix = "/_fs-ch-2fa"
	const image = "data:image/png;base64,iVBORw0KGgo="
	var verifyHits atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":false,"verificationMethod":"email-2fa"}`, testSessionHex())
		case "/v1/session/verify_code":
			verifyHits.Add(1)
			if _, err := r.Cookie("fk"); err != nil {
				w.WriteHeader(http.StatusNotAcceptable)
				return
			}
			fmt.Fprint(w, `{}`)
		case "/v1/oauth/authorization":
			http.NotFound(w, r) // use the legacy exchange below
		case "/v1/oauth/token":
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		case "/":
			if _, err := r.Cookie("fk"); err == nil {
				fmt.Fprint(w, "<html>signed in</html>")
				return
			}
			w.Header().Set("Set-Cookie", "fk=1; Path=/")
			fmt.Fprintf(w, `<html><body>Client Challenge <script src="%s/script.js"></script></body></html>`, prefix)
		case prefix + "/script.js":
			fmt.Fprintf(w, `init([{"ty":"captcha","data":{"image_b64":%q}}], "tok-1", %q)`, image, prefix)
		case prefix + "/fst-post-back":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"success"}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "2fa" {
		t.Fatalf("login_password = %+v", ls)
	}
	ls := ctrl.Login2FA(ctx, api.Login2FAPayload{Code: "123456"})
	if ls.Step != "captcha" || ls.CaptchaImage != image || ls.Error != "" {
		t.Fatalf("challenged verify = %+v, want the captcha step", ls)
	}
	if ls.VerificationMethod != "email-2fa" {
		t.Fatalf("captcha state verification method = %q, want email-2fa", ls.VerificationMethod)
	}
	ls = ctrl.LoginCaptcha(ctx, "right")
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("solved captcha = %+v, want a completed sign-in", ls)
	}
	if got := verifyHits.Load(); got < 2 {
		t.Fatalf("verify_code calls = %d, want the 406 and its solved retry", got)
	}
	if st := ctrl.Snapshot(); !st.Authenticated {
		t.Fatalf("not authenticated after the captcha: %+v", st)
	}
}

// The token exchange after a verified code is challenged too; the same run
// must surface the image and resume the exchange.
func TestLogin2FACaptchaOnTokenExchange(t *testing.T) {
	const prefix = "/_fs-ch-exchange"
	const image = "data:image/png;base64,iVBORw0KGgo="
	var tokenHits atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":false,"verificationMethod":"email-2fa"}`, testSessionHex())
		case "/v1/session/verify_code":
			fmt.Fprint(w, `{}`)
		case "/v1/oauth/authorization":
			if _, err := r.Cookie("fk"); err != nil {
				w.WriteHeader(http.StatusNotAcceptable)
				return
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode authorization: %v", err)
				return
			}
			state, _ := body["state"].(string)
			fmt.Fprintf(w, `{"code":"the-code","state":%q}`, state)
		case "/v1/token":
			tokenHits.Add(1)
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		case "/":
			if _, err := r.Cookie("fk"); err == nil {
				fmt.Fprint(w, "<html>signed in</html>")
				return
			}
			w.Header().Set("Set-Cookie", "fk=1; Path=/")
			fmt.Fprintf(w, `<html><body>Client Challenge <script src="%s/script.js"></script></body></html>`, prefix)
		case prefix + "/script.js":
			fmt.Fprintf(w, `init([{"ty":"captcha","data":{"image_b64":%q}}], "tok-1", %q)`, image, prefix)
		case prefix + "/fst-post-back":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"success"}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "2fa" {
		t.Fatalf("login_password = %+v", ls)
	}
	ls := ctrl.Login2FA(ctx, api.Login2FAPayload{Code: "123456"})
	if ls.Step != "captcha" || ls.CaptchaImage != image {
		t.Fatalf("challenged exchange = %+v, want the captcha step", ls)
	}
	ls = ctrl.LoginCaptcha(ctx, "right")
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("solved exchange captcha = %+v, want a completed sign-in", ls)
	}
	if got := tokenHits.Load(); got != 1 {
		t.Fatalf("token exchange calls = %d, want 1", got)
	}
}

// A resend asks Firefox Accounts for a fresh code and stays on the code
// screen; it must not submit an empty code as a verification attempt.
func TestLogin2FAResendCode(t *testing.T) {
	var resendCalls, verifyCalls atomic.Int32
	var resendAuth string
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":false,"verificationMethod":"email-2fa"}`, testSessionHex())
		case "/v1/session/resend_code":
			resendCalls.Add(1)
			resendAuth = r.Header.Get("Authorization")
			fmt.Fprint(w, `{}`)
		case "/v1/session/verify_code":
			verifyCalls.Add(1)
			fmt.Fprint(w, `{}`)
		case "/v1/oauth/authorization":
			http.NotFound(w, r)
		case "/v1/oauth/token":
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "2fa" {
		t.Fatalf("login_password = %+v", ls)
	}
	ls := ctrl.Login2FA(ctx, api.Login2FAPayload{Resend: true})
	if ls.Step != "2fa" || !ls.CodeSent || ls.Error != "" {
		t.Fatalf("resend = %+v, want the code screen with code_sent", ls)
	}
	if resendCalls.Load() != 1 || verifyCalls.Load() != 0 {
		t.Fatalf("resend calls = %d, verify calls = %d; want 1 and 0", resendCalls.Load(), verifyCalls.Load())
	}
	if !strings.HasPrefix(resendAuth, "Hawk ") {
		t.Fatalf("resend Authorization = %q, want a Hawk session signature", resendAuth)
	}

	ls = ctrl.Login2FA(ctx, api.Login2FAPayload{Code: "123456"})
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("login after resend = %+v", ls)
	}
	if verifyCalls.Load() != 1 {
		t.Fatalf("verify calls = %d, want 1", verifyCalls.Load())
	}
}

// Password sign-in now exchanges the verified session for an authorization
// code and redeems it at the OAuth form endpoint. The legacy fxa-credentials
// route is not touched when that path works.
func TestLoginUsesAuthorizationCodeExchange(t *testing.T) {
	var legacyCalls, tokenCalls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionHex())
		case "/v1/oauth/authorization":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode authorization: %v", err)
				return
			}
			state, _ := body["state"].(string)
			fmt.Fprintf(w, `{"code":"the-code","state":%q}`, state)
		case "/v1/token":
			tokenCalls.Add(1)
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		case "/v1/oauth/token":
			legacyCalls.Add(1)
			w.WriteHeader(http.StatusNotAcceptable)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "done" || ls.Error != "" {
		t.Fatalf("login = %+v, want a completed sign-in", ls)
	}
	if tokenCalls.Load() != 1 || legacyCalls.Load() != 0 {
		t.Fatalf("token calls = %d, legacy calls = %d; want 1 and 0", tokenCalls.Load(), legacyCalls.Load())
	}
}

// When both token exchanges answer the empty Fastly hard block, the password
// form must not dead-end: it continues in the browser flow instead.
func TestLoginTokenExchangeHardBlockFallsBackToBrowser(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionHex())
		case "/v1/oauth/authorization", "/v1/oauth/token":
			w.WriteHeader(http.StatusNotAcceptable)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	ls := ctrl.LoginPassword(ctx, "pw")
	if ls.Step != "browser" || ls.URL == "" || ls.Error != "" {
		t.Fatalf("login = %+v, want the browser fallback", ls)
	}
	if st := ctrl.Snapshot(); st.Login == nil || st.Login.Step != "browser" {
		t.Fatalf("snapshot login = %+v, want the browser flow", st.Login)
	}
}

// A verified session whose token exchange failed can retry the exchange
// without re-entering a code the server has already consumed.
func TestLogin2FATokenRetryAfterVerifiedCode(t *testing.T) {
	var verifyCalls, tokenCalls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":false,"verificationMethod":"email-2fa"}`, testSessionHex())
		case "/v1/session/verify_code":
			verifyCalls.Add(1)
			fmt.Fprint(w, `{}`)
		case "/v1/oauth/authorization":
			http.NotFound(w, r)
		case "/v1/oauth/token":
			if tokenCalls.Add(1) == 1 {
				http.Error(w, `{"errno":999,"message":"temporary"}`, http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "2fa" {
		t.Fatalf("login_password = %+v", ls)
	}
	ls := ctrl.Login2FA(ctx, api.Login2FAPayload{Code: "123456"})
	if ls.Step != "2fa" || ls.Error == "" {
		t.Fatalf("first submit = %+v, want the token exchange error", ls)
	}
	ls = ctrl.Login2FA(ctx, api.Login2FAPayload{Code: "123456"})
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("retry = %+v, want a completed sign-in", ls)
	}
	if verifyCalls.Load() != 1 || tokenCalls.Load() != 2 {
		t.Fatalf("verify calls = %d, token calls = %d; want 1 and 2", verifyCalls.Load(), tokenCalls.Load())
	}
}

// A verified password session whose token exchange failed returns to the
// password step, and a retry skips the login and the token exchange's
// verification because the session is already authenticated.
func TestLoginPasswordTokenRetryWithoutCode(t *testing.T) {
	var loginCalls, verifyCalls, tokenCalls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			loginCalls.Add(1)
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":true}`, testSessionHex())
		case "/v1/oauth/authorization":
			http.NotFound(w, r)
		case "/v1/oauth/token":
			if tokenCalls.Add(1) == 1 {
				http.Error(w, `{"errno":999,"message":"temporary"}`, http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		case "/v1/session/verify_code":
			verifyCalls.Add(1)
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	ls := ctrl.LoginPassword(ctx, "pw")
	if ls.Step != "password" || ls.Error == "" {
		t.Fatalf("login = %+v, want the password step with the exchange error", ls)
	}
	ls = ctrl.Login2FA(ctx, api.Login2FAPayload{})
	if ls.Step != "done" || ls.Error != "" {
		t.Fatalf("retry = %+v, want a completed sign-in", ls)
	}
	if loginCalls.Load() != 1 || verifyCalls.Load() != 0 || tokenCalls.Load() != 2 {
		t.Fatalf("login calls = %d, verify calls = %d, token calls = %d; want 1, 0 and 2",
			loginCalls.Load(), verifyCalls.Load(), tokenCalls.Load())
	}
}

// A resend would send a code the email-link flow cannot use, so the engine
// refuses it without calling the endpoint.
func TestLogin2FAResendRejectedForEmailLink(t *testing.T) {
	var resendCalls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/login":
			fmt.Fprintf(w, `{"sessionToken":%q,"verified":false,"verificationMethod":"email"}`, testSessionHex())
		case "/v1/session/resend_code":
			resendCalls.Add(1)
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}
	ctrl, _ := newTestController(t, handler)
	ctx := context.Background()

	ctrl.LoginEmail("user@example.com")
	if ls := ctrl.LoginPassword(ctx, "pw"); ls.Step != "2fa" || ls.VerificationMethod != "email" {
		t.Fatalf("login_password = %+v", ls)
	}
	ls := ctrl.Login2FA(ctx, api.Login2FAPayload{Resend: true})
	if ls.Step != "2fa" || !strings.Contains(ls.Error, "email link") {
		t.Fatalf("resend = %+v, want a refused link resend", ls)
	}
	if resendCalls.Load() != 0 {
		t.Fatalf("resend calls = %d, want 0", resendCalls.Load())
	}
}
