package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
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

	ls = ctrl.Login2FA(ctx, "ABCD-1234")
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
	ls = ctrl.Login2FA(ctx, "123456")
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
