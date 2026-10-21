// Package mobile is the gomobile facade over the kumiho engine. Kotlin calls
// the five exported functions (Start, Command, Palette, Version and Close)
// through the generated Mobile class; everything else is unexported so the
// JNI surface stays as small as possible.
//
// It is a thin dispatcher: the engine, the protocol DTOs and the platform
// provider are the same ones the desktop daemon uses. The facade adds the
// Android-specific wiring (app-private paths, the VpnService device bridge,
// the route exclusions and the doctor facts) and nothing about tunnels.
//
// The command envelope matches internal/ipc: a request is
// {"type":"...","payload":{...}} and a response is the same shape, while a
// domain failure is {"error":{"code":"...","message":"..."}}. A Go error is
// returned only for facade failures (not started, malformed request, panic);
// Kotlin therefore keeps the domain error code.
package mobile
