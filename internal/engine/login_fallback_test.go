package engine

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// A Fastly hard block on the password API must not dead-end sign-in: the
// controller starts the equivalent browser flow, which runs Mozilla's own
// page and therefore survives any challenge change.
func TestLoginPasswordChallengeStartsBrowserFallback(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account/login" {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		http.NotFound(w, r)
	})
	ctrl.fxa.Solver = nil

	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login_email = %+v", st)
	}
	st := ctrl.LoginPassword(context.Background(), "pw")
	if st.Step != "browser" {
		t.Fatalf("login_password step = %q, want browser; state = %+v", st.Step, st)
	}
	if st.Method != MethodBrowser {
		t.Errorf("method = %q, want %q", st.Method, MethodBrowser)
	}
	if st.URL == "" || st.LoginID == "" {
		t.Fatalf("browser fallback missing url/id: %+v", st)
	}
	if st.Error != "" {
		t.Fatalf("browser fallback must be a healthy flow, got error %q", st.Error)
	}
	if ctrl.pending != nil {
		t.Fatal("password pending state survived the fallback")
	}
	if ctrl.browser == nil || !ctrl.browser.active || ctrl.browser.id != st.LoginID {
		t.Fatalf("browser flow is not active: %+v", ctrl.browser)
	}
	if err := ctrl.LoginCancel(st.LoginID); err != nil {
		t.Fatalf("cancel browser fallback: %v", err)
	}
}

// A normal credential rejection is not a challenge: it must stay on the
// password step so the user can correct the password.
func TestLoginPasswordRejectionDoesNotStartBrowser(t *testing.T) {
	ctrl, _ := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account/login" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"code":400,"errno":102,"message":"incorrect password"}`)
			return
		}
		http.NotFound(w, r)
	})
	ctrl.fxa.Solver = nil

	if st := ctrl.LoginEmail("user@example.com"); st.Step != "password" {
		t.Fatalf("login_email = %+v", st)
	}
	st := ctrl.LoginPassword(context.Background(), "pw")
	if st.Step != "password" || st.Error != "incorrect password" {
		t.Fatalf("login_password = %+v, want the API rejection on the password step", st)
	}
	if ctrl.browser != nil {
		t.Fatalf("browser flow started for a normal rejection: %+v", ctrl.browser)
	}
}
