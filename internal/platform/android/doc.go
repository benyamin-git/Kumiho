// Package android implements the Android platform.Provider (D13). The Kotlin
// VpnService owns the tunnel interface: it creates it through
// VpnService.Builder, applies the addresses, routes, DNS and split tunnel,
// then hands the descriptor to the engine through the Bridge. The provider
// adopts that descriptor, drops IPv6 reads in userspace (C5) and leaves all
// network configuration to the builder.
//
// The engine runs in-process inside the app (D8); the standalone daemon
// binary is not used on Android.
package android
