# R8 rules for the Kumiho release build.
#
# The app entry points (Application, Activity, Service) are kept through the
# manifest merge. The rules below protect the gomobile AAR built by
# scripts/android-build-aar.sh: the generated Java class and interface
# implementations the Go runtime reflects on, and every JNI entry point. The
# VPN service and its notifications land in T9.
#
# The AAR also ships consumer rules (go.** and dev.benyamin.kumiho.go.**) that
# AGP merges; they are repeated here so a release build never depends on the
# AAR's proguard.txt being present. The proxied interfaces live in
# dev.benyamin.kumiho.go.mobile (gobind appends the Go package name to the
# -javapkg prefix).
-keep class dev.benyamin.kumiho.go.** { *; }
-keep class * implements dev.benyamin.kumiho.go.mobile.DeviceBridge { *; }
-keep class * implements dev.benyamin.kumiho.go.mobile.Listener { *; }
-keepclasseswithmembers,includedescriptorclasses class * { native <methods>; }
