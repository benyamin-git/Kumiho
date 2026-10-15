//go:build tools

// Package tools pins dependencies that no regular package imports yet so
// they stay in go.mod and the vendor tree. It will hold the WebView2 binding
// until the Windows GUI commands import it directly.
package tools

import _ "github.com/jchv/go-webview2"
