# Source this file to configure the kumiho Android build toolchain:
#   . scripts/android-env.sh
# It only sets environment variables; install the toolchain with:
#   bash scripts/android-setup.sh
# See .superpowers (android client plan) for JDK 17 / SDK 36 / NDK 28.2 pins.

KUMIHO_ANDROID_HOME="$HOME/Android"
export ANDROID_HOME="${ANDROID_HOME:-$KUMIHO_ANDROID_HOME/Sdk}"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/28.2.13676358"

# JDK 17: honor an explicit JAVA_HOME, else the system OpenJDK when present,
# else the user-local Temurin JDK installed by android-setup.sh (for boxes
# without a system JDK and without sudo, handled by the T1 controller ruling).
JAVA_HOME="${JAVA_HOME:-/usr/lib/jvm/java-17-openjdk-amd64}"
if [ ! -d "$JAVA_HOME" ] && [ ! -x "$JAVA_HOME/bin/java" ] && [ -d "$KUMIHO_ANDROID_HOME/jdk-17" ]; then
	JAVA_HOME="$KUMIHO_ANDROID_HOME/jdk-17"
fi
export JAVA_HOME

case ":$PATH:" in
*":$HOME/go/bin:"*) ;;
*) PATH="$HOME/go/bin:$PATH" ;;
esac
for dir in "$JAVA_HOME/bin" "$ANDROID_HOME/cmdline-tools/latest/bin" \
	"$ANDROID_HOME/platform-tools" "$ANDROID_HOME/emulator"; do
	if [ -d "$dir" ]; then
		case ":$PATH:" in
		*":$dir:"*) ;;
		*) PATH="$dir:$PATH" ;;
		esac
	fi
done
export PATH
unset KUMIHO_ANDROID_HOME
echo "android env: JAVA_HOME=$JAVA_HOME ANDROID_HOME=$ANDROID_HOME"
