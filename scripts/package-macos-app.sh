#!/bin/sh
# 把 cronwatch 包成一個可以拖進「應用程式」的 macOS app，再包成一張 DMG。
#
# 產出：
#   dist/CrontabWatcher.app   —— 可直接執行的 app bundle
#   dist/CrontabWatcher.dmg   —— 打開後看得到 app 與「應用程式」捷徑，拖過去即完成安裝
#
# 用法：scripts/package-macos-app.sh <version>
set -eu

APP_NAME="CrontabWatcher"
BUNDLE_IDENTIFIER="com.jameshsueh.crontab-watcher"
VERSION="${1:-0.1.0}"

PROJECT_DIRECTORY="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIRECTORY="${PROJECT_DIRECTORY}/dist"
APP_DIRECTORY="${DIST_DIRECTORY}/${APP_NAME}.app"
CONTENTS_DIRECTORY="${APP_DIRECTORY}/Contents"
STAGING_DIRECTORY="${DIST_DIRECTORY}/dmg"
DMG_PATH="${DIST_DIRECTORY}/${APP_NAME}.dmg"

if [ "$(uname -s)" != "Darwin" ]; then
    echo "package-macos-app: this only builds on macOS" >&2
    exit 1
fi

# app 名稱不得含空白：這個執行檔的路徑會被寫進 crontab 條目，而 crontab 是以
# 空白分欄的。名稱裡有一個空格，寫出去的排程就再也跑不起來。
case "${APP_NAME}" in
    *\ *) echo "package-macos-app: the app name must not contain spaces" >&2; exit 1 ;;
esac

echo "==> cleaning ${DIST_DIRECTORY}"
rm -rf "${DIST_DIRECTORY}"
mkdir -p "${CONTENTS_DIRECTORY}/MacOS" "${CONTENTS_DIRECTORY}/Resources"

echo "==> building the binary"
# 不加 CGO_ENABLED=0：選單列與視窗都是 Cocoa，非 cgo 不可。
( cd "${PROJECT_DIRECTORY}" && go build -trimpath -ldflags="-s -w" \
    -o "${CONTENTS_DIRECTORY}/MacOS/cronwatch" ./cmd/cronwatch )

echo "==> drawing the icon"
ICONSET_DIRECTORY="${DIST_DIRECTORY}/${APP_NAME}.iconset"
( cd "${PROJECT_DIRECTORY}" && go run ./tools/appicon "${ICONSET_DIRECTORY}" )
iconutil -c icns "${ICONSET_DIRECTORY}" -o "${CONTENTS_DIRECTORY}/Resources/AppIcon.icns"
rm -rf "${ICONSET_DIRECTORY}"

echo "==> writing Info.plist"
cat > "${CONTENTS_DIRECTORY}/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>            <string>${APP_NAME}</string>
    <key>CFBundleDisplayName</key>     <string>${APP_NAME}</string>
    <key>CFBundleIdentifier</key>      <string>${BUNDLE_IDENTIFIER}</string>
    <key>CFBundleVersion</key>         <string>${VERSION}</string>
    <key>CFBundleShortVersionString</key> <string>${VERSION}</string>
    <key>CFBundlePackageType</key>     <string>APPL</string>
    <key>CFBundleExecutable</key>      <string>cronwatch</string>
    <key>CFBundleIconFile</key>        <string>AppIcon</string>
    <key>LSMinimumSystemVersion</key>  <string>11.0</string>
    <!-- 選單列 app 不該佔一個 Dock 圖示。視窗子程序會在自己那一端宣告成前景
         app，所以視窗照樣拿得到鍵盤焦點。 -->
    <key>LSUIElement</key>             <true/>
    <key>NSHighResolutionCapable</key> <true/>
    <key>NSHumanReadableCopyright</key> <string>James Hsueh</string>
</dict>
</plist>
PLIST
plutil -lint "${CONTENTS_DIRECTORY}/Info.plist" > /dev/null

printf 'APPL????' > "${CONTENTS_DIRECTORY}/PkgInfo"

echo "==> signing (ad-hoc)"
# 本機自用，沒有開發者憑證。ad-hoc 簽章讓 macOS 認得這是一個穩定的身分——
# 沒有簽章的話，每次重新編譯都會被當成不同的 app，已經給過的權限會被忘掉。
codesign --force --deep --sign - "${APP_DIRECTORY}"
codesign --verify --deep --strict "${APP_DIRECTORY}"

echo "==> building the disk image"
mkdir -p "${STAGING_DIRECTORY}"
cp -R "${APP_DIRECTORY}" "${STAGING_DIRECTORY}/"
ln -s /Applications "${STAGING_DIRECTORY}/Applications"

hdiutil create \
    -volname "${APP_NAME}" \
    -srcfolder "${STAGING_DIRECTORY}" \
    -ov -format UDZO -quiet \
    "${DMG_PATH}"

rm -rf "${STAGING_DIRECTORY}"

echo
echo "${APP_DIRECTORY}"
echo "${DMG_PATH}"
echo
echo "Open the disk image and drag ${APP_NAME} onto Applications."
