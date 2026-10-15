/* Kumiho bridge transport: one API over both carriers.
 *
 * The shell (Windows, T1.7) and the dev harness (T1.10) carry the same JSON
 * envelopes over different pipes; the rest of the UI only sees this module:
 *
 *   send(name, payload)  Promise<string>   UI -> host
 *   subscribe(fn)        unsubscribe       host -> UI state envelopes
 *
 * `send` resolves with "" when the host accepted the action — accepted, NOT
 * completed (connect/disconnect may take up to 90 s and finish through the
 * state stream) — and with a non-empty error message when it was refused.
 *
 * Shell mode (window.kumihoAction exists): send() calls the WebView2 binding
 * `kumihoAction(<envelope JSON string>)`; the host pushes state by evaluating
 * `window.kumihoReceive(<envelope object>)` — the envelope is interpolated
 * into the eval as a JS object literal (host_windows.go), so receive() must
 * accept objects; the harness SSE carrier passes the same envelope as a
 * string.
 *
 * Harness mode (no binding; the Linux dev harness): send() POSTs the same
 * envelope to `action` ({"ok":true} = accepted, {"error":…} = refused) and
 * state arrives on the SSE stream `events`. WebSocket is deliberately not
 * used (T1.10).
 *
 * This file is loaded as a classic script in <head> (not a module) so
 * window.kumihoReceive exists as early as possible: the host may evaluate a
 * push while the document is still parsing. The last state received before
 * anyone subscribed is buffered and replayed to the first subscriber, so a
 * push can never land in a gap. URLs are relative to the document, which
 * keeps the server's per-run token segment intact.
 */
(function () {
  "use strict";

  var listeners = new Set();
  var lastState = null;

  // receive(frame) takes one host -> UI frame from either carrier and fans it
  // out; a state that arrives before the first subscriber is buffered (see
  // above). The shell (host_windows.go) evaluates the envelope as a JS object
  // literal, so an object is used as-is; the harness SSE data is a string and
  // is parsed. Anything else keeps the malformed-frame path.
  function receive(frame) {
    var envelope;
    if (typeof frame === "string") {
      try {
        envelope = JSON.parse(frame);
      } catch (err) {
        console.error("kumiho: malformed state frame", err);
        return;
      }
    } else if (frame !== null && typeof frame === "object") {
      envelope = frame;
    } else {
      console.error("kumiho: malformed state frame", frame);
      return;
    }
    if (!envelope || envelope.type !== "state" || typeof envelope.state !== "object" || envelope.state === null) {
      console.error("kumiho: unexpected frame", envelope && envelope.type);
      return;
    }
    lastState = envelope.state;
    listeners.forEach(function (listener) {
      listener(lastState);
    });
  }

  // The host -> UI entry point the shell evaluates. Defined before app boot.
  window.kumihoReceive = receive;

  function subscribe(listener) {
    listeners.add(listener);
    if (lastState !== null) listener(lastState);
    return function () {
      listeners.delete(listener);
    };
  }

  function sendShell(envelope) {
    try {
      return Promise.resolve(window.kumihoAction(envelope)).then(
        function (result) {
          return typeof result === "string" ? result : "";
        },
        function (err) {
          return "the action was refused: " + String(err);
        }
      );
    } catch (err) {
      return Promise.resolve("the action was refused: " + String(err));
    }
  }

  function sendHarness(envelope) {
    return fetch("action", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: envelope
    }).then(
      function (res) {
        return res.text().then(function (text) {
          if (res.ok) return "";
          var message = "action rejected (" + res.status + ")";
          try {
            var body = JSON.parse(text);
            if (body && typeof body.error === "string" && body.error) message = body.error;
          } catch (err) {
            // Non-JSON error body; keep the status message.
          }
          return message;
        });
      },
      function () {
        return "the host bridge is unreachable";
      }
    );
  }

  var mode = typeof window.kumihoAction === "function" ? "shell" : "harness";

  function send(name, payload) {
    var envelope = JSON.stringify({ type: "action", name: name, payload: payload || {} });
    return mode === "shell" ? sendShell(envelope) : sendHarness(envelope);
  }

  if (mode === "harness") {
    // The harness streams state envelopes on GET events; EventSource
    // reconnects on its own after a dropped connection.
    var source = new EventSource("events");
    source.onmessage = function (event) {
      receive(event.data);
    };
    source.onerror = function () {
      // EventSource stays in CONNECTING and retries; nothing to do here.
    };
  }

  window.KumihoTransport = { mode: mode, send: send, subscribe: subscribe };
})();
