#!/bin/zsh
set -euo pipefail

usage() {
  print -u2 'usage: scripts/testflight.sh <parent|child> <archive|upload>'
  exit 2
}

[[ $# -eq 2 ]] || usage
app="$1"
action="$2"

case "$app" in
  parent)
    project_dir="ios/CooperTheCop"
    scheme="CooperTheCop"
    ;;
  child)
    project_dir="ios/CooperWatch"
    scheme="CooperWatch"
    ;;
  *) usage ;;
esac

case "$action" in
  archive|upload) ;;
  *) usage ;;
esac

command -v xcodegen >/dev/null || {
  print -u2 'xcodegen is required'
  exit 1
}

output_dir="${COOP_RELEASE_DIR:-$PWD/build/app-store}/$scheme"
archive_path="$output_dir/$scheme.xcarchive"
mkdir -p "$output_dir"

xcodegen generate --spec "$project_dir/project.yml"
xcodebuild \
  -skipPackagePluginValidation \
  -project "$project_dir/$scheme.xcodeproj" \
  -scheme "$scheme" \
  -destination 'generic/platform=iOS' \
  -archivePath "$archive_path" \
  archive

[[ "$action" == upload ]] || {
  print "archive created at $archive_path"
  exit 0
}

: "${APP_STORE_CONNECT_KEY_ID:?set APP_STORE_CONNECT_KEY_ID}"
: "${APP_STORE_CONNECT_ISSUER_ID:?set APP_STORE_CONNECT_ISSUER_ID}"
: "${APP_STORE_CONNECT_KEY_PATH:?set APP_STORE_CONNECT_KEY_PATH}"

export_options="$output_dir/ExportOptions.plist"
command /usr/bin/plutil -create xml1 "$export_options"
command /usr/bin/plutil -insert method -string app-store-connect "$export_options"
command /usr/bin/plutil -insert destination -string upload "$export_options"
command /usr/bin/plutil -insert signingStyle -string automatic "$export_options"

xcodebuild \
  -exportArchive \
  -archivePath "$archive_path" \
  -exportOptionsPlist "$export_options" \
  -exportPath "$output_dir/export" \
  -allowProvisioningUpdates \
  -authenticationKeyPath "$APP_STORE_CONNECT_KEY_PATH" \
  -authenticationKeyID "$APP_STORE_CONNECT_KEY_ID" \
  -authenticationKeyIssuerID "$APP_STORE_CONNECT_ISSUER_ID"

print "$scheme uploaded to App Store Connect"
