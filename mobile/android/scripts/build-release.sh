#!/usr/bin/env bash
set -euo pipefail
android_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
repo_dir=$(cd -- "$android_dir/../.." && pwd)
: "${VERSION:?Set VERSION to the release tag vMAJOR.MINOR.PATCH.}"
: "${ANDROID_HOME:=${ANDROID_SDK_ROOT:-}}"
: "${ANDROID_HOME:?Set ANDROID_HOME to the pinned SDK.}"
: "${AGENTNET_PREVIEW_KEYSTORE:?The original preview signing keystore is required.}"
[[ $VERSION =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] || { printf '%s\n' 'Invalid release version.' >&2; exit 2; }
[[ -f $AGENTNET_PREVIEW_KEYSTORE ]] || { printf '%s\n' 'Missing original preview signing key.' >&2; exit 1; }
export VERSION ANDROID_HOME AGENTNET_PREVIEW_KEYSTORE
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/28.2.13676358"
export AGENTNET_RELEASE_VERSION="$VERSION"
export ANDROID_ABIS=android/arm64,android/amd64
"$android_dir/scripts/build-core.sh" --bootstrap
cd "$android_dir"
./gradlew --no-daemon --max-workers=2 :app:testPreviewReleaseUnitTest :app:lintPreviewRelease :app:assemblePreviewRelease
apk="$android_dir/app/build/outputs/apk/previewRelease/app-previewRelease.apk"
"$android_dir/scripts/verify-apk.sh" "$apk" "$VERSION"
mkdir -p "$repo_dir/dist"
cp -- "$apk" "$repo_dir/dist/AgentNet-android.apk"
sha256sum "$repo_dir/dist/AgentNet-android.apk"
