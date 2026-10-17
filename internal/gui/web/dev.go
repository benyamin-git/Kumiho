package web

// Dev-only mock control panel (T1.10). These endpoints exist for the dev
// harness and the T1.11 capture script; they are active only when the server
// runs with Options.Dev (the harness's mode). Otherwise every /dev path —
// claimed by the harness route — answers a plain 404, so the dev surface
// never exists in the shell or a non-dev server.
//
// Contract (for T1.11; JSON request bodies, {"ok":true} or the harness's
// {"error":"..."} response for every endpoint):
//
//	GET  /dev            the self-contained control panel page
//	POST /dev/scenario   {"scenario":"disconnected|connected|connecting|error|signed-out"}
//	                     switches the mock's reported state
//	POST /dev/latency    {"ms":800}      delays every mock call; 0 disables
//	POST /dev/fail       {"on":true,"message":"guardian: HTTP 503"} arms the
//	                     next mock call to fail (empty message uses a fixed
//	                     sample); {"on":false} clears a pending failure
//	POST /dev/log        {"message":"..."} emits one live info log entry
//	                     (an empty body uses a fixed sample message); visible
//	                     once the UI has subscribed to the logs
//
// Every endpoint is POST-only apart from GET /dev; unknown /dev paths answer
// 404 and a wrong method answers 405 with the matching Allow header.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// devScenarios are the accepted /dev/scenario names, in the mock's
// vocabulary. Unknown names are rejected: the panel and the capture script
// should fail loudly, not fall silently into the disconnected state the mock
// itself would default to.
var devScenarios = []string{"disconnected", "connected", "connecting", "error", "signed-out"}

// Fixed sample texts the endpoints substitute when a request carries none.
const (
	devFailDefaultMessage = "dev: injected failure"
	devLogDefaultMessage  = "dev: sample log entry"
)

// serveDev routes the claimed /dev namespace.
func (h *Harness) serveDev(w http.ResponseWriter, r *http.Request, p string) {
	if !h.dev {
		http.NotFound(w, r)
		return
	}
	switch p {
	case pathDev:
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.serveDevPage(w)
	case pathDev + "/scenario":
		h.serveDevPost(w, r, h.devScenario)
	case pathDev + "/latency":
		h.serveDevPost(w, r, h.devLatency)
	case pathDev + "/fail":
		h.serveDevPost(w, r, h.devFail)
	case pathDev + "/log":
		h.serveDevPost(w, r, h.devLog)
	default:
		httpError(w, http.StatusNotFound, "unknown dev endpoint")
	}
}

// serveDevPost enforces the dev endpoints' shared policy — POST only, one
// bounded JSON body, no request parameters — then runs handle and answers
// {"ok":true}; a handler error is a 400.
func (h *Harness) serveDevPost(w http.ResponseWriter, r *http.Request, handle func(raw []byte) error) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	raw, err := readBody(w, r)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := handle(raw); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

// devScenarioRequest is POST /dev/scenario's body.
type devScenarioRequest struct {
	Scenario string `json:"scenario"`
}

// devScenario implements POST /dev/scenario.
func (h *Harness) devScenario(raw []byte) error {
	req, err := decodePayload[devScenarioRequest](raw)
	if err != nil {
		return fmt.Errorf("decode scenario: %w", err)
	}
	for _, name := range devScenarios {
		if req.Scenario == name {
			h.cfg.DevControl.SetScenario(name)
			return nil
		}
	}
	return fmt.Errorf("unknown scenario %q", req.Scenario)
}

// devLatencyRequest is POST /dev/latency's body.
type devLatencyRequest struct {
	MS int `json:"ms"`
}

// devLatency implements POST /dev/latency.
func (h *Harness) devLatency(raw []byte) error {
	req, err := decodePayload[devLatencyRequest](raw)
	if err != nil {
		return fmt.Errorf("decode latency: %w", err)
	}
	if req.MS < 0 {
		return errors.New("latency ms must be >= 0")
	}
	h.cfg.DevControl.SetLatency(time.Duration(req.MS) * time.Millisecond)
	return nil
}

// devFailRequest is POST /dev/fail's body.
type devFailRequest struct {
	On      bool   `json:"on"`
	Message string `json:"message"`
}

// devFail implements POST /dev/fail: {"on":true} arms a failure for the next
// mock call (consumed once, like mock.FailNext), {"on":false} clears one.
func (h *Harness) devFail(raw []byte) error {
	req, err := decodePayload[devFailRequest](raw)
	if err != nil {
		return fmt.Errorf("decode fail: %w", err)
	}
	if !req.On {
		h.cfg.DevControl.FailNext(nil)
		return nil
	}
	msg := req.Message
	if msg == "" {
		msg = devFailDefaultMessage
	}
	h.cfg.DevControl.FailNext(errors.New(msg))
	return nil
}

// devLogRequest is POST /dev/log's body.
type devLogRequest struct {
	Message string `json:"message"`
}

// devLog implements POST /dev/log.
func (h *Harness) devLog(raw []byte) error {
	req, err := decodePayload[devLogRequest](raw)
	if err != nil {
		return fmt.Errorf("decode log: %w", err)
	}
	msg := req.Message
	if msg == "" {
		msg = devLogDefaultMessage
	}
	h.cfg.DevControl.EmitLog(msg)
	return nil
}

// serveDevPage renders GET /dev: the self-contained control panel. Inline
// style and script only, each gated by a fresh per-response nonce; every
// colour comes from the theme block the server injects into index.html (no
// raw hex lives here; see internal/gui/theme).
func (h *Harness) serveDevPage(w http.ResponseWriter) {
	nonce, err := newNonce()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+
		nonce+"'; script-src 'nonce-"+nonce+"'; connect-src 'self'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.WriteString(w, devPage(nonce, h.themeCSS())); err != nil {
		return
	}
}

// devPageHTML is the dev panel document. {{NONCE}} appears in the style and
// script elements, {{THEME}} is the server's theme block (its
// "</style" sequences are already stripped by SetThemeCSS, so the
// substitution cannot break out of the element).
const devPageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Kumiho web harness</title>
<style nonce="{{NONCE}}">{{THEME}}
body{margin:0;padding:24px;background:var(--background);color:var(--on-surface);font:14px/1.5 system-ui,sans-serif}
h1{font-size:20px;margin:0 0 4px}
p{margin:0 0 16px;color:var(--outline)}
a{color:var(--primary)}
section{max-width:760px;background:var(--surface-container-1);border:1px solid var(--outline);border-radius:8px;padding:12px 16px;margin:0 0 12px}
h2{font-size:13px;margin:0 0 8px;font-weight:600;letter-spacing:.04em;text-transform:uppercase;color:var(--outline)}
button{min-height:32px;margin:0 8px 8px 0;padding:0 12px;border:1px solid var(--outline);border-radius:6px;background:var(--primary-container);color:var(--on-primary-container);font:inherit;cursor:pointer}
button:hover{background:var(--secondary-container)}
button:focus-visible{outline:2px solid var(--primary);outline-offset:2px}
input{min-height:30px;margin:0 8px 8px 0;padding:0 8px;border:1px solid var(--outline);border-radius:6px;background:var(--background);color:var(--on-surface);font:inherit}
pre{max-width:760px;margin:0;padding:10px 12px;background:var(--surface-container-1);border:1px solid var(--outline);border-radius:8px;white-space:pre-wrap}
</style>
</head>
<body>
<h1>Kumiho web harness</h1>
<p>Mock daemon control panel. <a href="/">Open the UI</a>.</p>
<section>
<h2>Scenario</h2>
<button data-scenario="disconnected">disconnected</button>
<button data-scenario="connected">connected</button>
<button data-scenario="connecting">connecting</button>
<button data-scenario="error">error</button>
<button data-scenario="signed-out">signed-out</button>
</section>
<section>
<h2>Latency</h2>
<button data-latency="0">0 ms</button>
<button data-latency="300">300 ms</button>
<button data-latency="1000">1 s</button>
<button data-latency="3000">3 s</button>
<input id="latency-value" type="number" min="0" step="50" value="0" aria-label="custom latency in milliseconds">
<button id="latency-custom">set</button>
</section>
<section>
<h2>Fail the next daemon call</h2>
<input id="fail-message" type="text" placeholder="message (optional)" size="30">
<button id="fail-arm">arm</button>
<button id="fail-clear">clear</button>
</section>
<section>
<h2>Emit a log entry</h2>
<input id="log-message" type="text" placeholder="message (optional)" size="30">
<button id="log-emit">emit</button>
</section>
<pre id="out">ready</pre>
<script nonce="{{NONCE}}">
"use strict";
var out = document.getElementById("out");
function show(path, text) { out.textContent = path + " -> " + text; }
async function post(path, body) {
  try {
    var res = await fetch(path, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(body || {})
    });
    show(path, res.status + " " + (await res.text()).trim());
  } catch (err) {
    show(path, String(err));
  }
}
document.querySelectorAll("[data-scenario]").forEach(function (b) {
  b.addEventListener("click", function () { post("/dev/scenario", {scenario: b.dataset.scenario}); });
});
document.querySelectorAll("[data-latency]").forEach(function (b) {
  b.addEventListener("click", function () { post("/dev/latency", {ms: Number(b.dataset.latency)}); });
});
document.getElementById("latency-custom").addEventListener("click", function () {
  post("/dev/latency", {ms: Number(document.getElementById("latency-value").value)});
});
document.getElementById("fail-arm").addEventListener("click", function () {
  post("/dev/fail", {on: true, message: document.getElementById("fail-message").value});
});
document.getElementById("fail-clear").addEventListener("click", function () { post("/dev/fail", {on: false}); });
document.getElementById("log-emit").addEventListener("click", function () {
  post("/dev/log", {message: document.getElementById("log-message").value});
});
</script>
</body>
</html>
`

// devPage renders the panel document with the nonce and theme block filled
// in.
func devPage(nonce, themeCSS string) string {
	page := strings.ReplaceAll(devPageHTML, "{{NONCE}}", nonce)
	return strings.ReplaceAll(page, "{{THEME}}", themeCSS)
}
