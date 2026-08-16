#!/bin/zsh
set -euo pipefail

usage() {
  print -u2 'usage: scripts/ota.sh build'
  exit 2
}

[[ $# -eq 1 && "$1" == build ]] || usage

repo_root="${0:A:h:h}"
config_file="$repo_root/deploy/ota/.env"
[[ -f "$config_file" ]] || {
  print -u2 'deploy/ota/.env is missing; copy deploy/ota/.env.example and configure it'
  exit 1
}

set -a
source "$config_file"
set +a

: "${COOP_DEVELOPMENT_TEAM:?set COOP_DEVELOPMENT_TEAM in deploy/ota/.env}"

build_root="$repo_root/.build/ota"
package_dir="$build_root/packages"

require_command() {
  command -v "$1" >/dev/null || {
    print -u2 "$1 is required"
    exit 1
  }
}

write_export_options() {
  local output="$build_root/ExportOptions.plist"
  /usr/bin/plutil -create xml1 "$output"
  /usr/bin/plutil -insert destination -string export "$output"
  /usr/bin/plutil -insert method -string release-testing "$output"
  /usr/bin/plutil -insert signingStyle -string automatic "$output"
  /usr/bin/plutil -insert teamID -string "$COOP_DEVELOPMENT_TEAM" "$output"
}

build_app() {
  local project_dir="$1"
  local scheme="$2"
  local ipa_name="$3"
  local archive="$build_root/$scheme.xcarchive"
  local export_dir="$build_root/export/$scheme"

  xcodegen generate --spec "$repo_root/$project_dir/project.yml"
  xcodebuild \
    -skipPackagePluginValidation \
    -project "$repo_root/$project_dir/$scheme.xcodeproj" \
    -scheme "$scheme" \
    -configuration Release \
    -destination 'generic/platform=iOS' \
    -archivePath "$archive" \
    -allowProvisioningUpdates \
    archive \
    DEVELOPMENT_TEAM="$COOP_DEVELOPMENT_TEAM" \
    CODE_SIGN_STYLE=Automatic

  xcodebuild \
    -exportArchive \
    -archivePath "$archive" \
    -exportOptionsPlist "$build_root/ExportOptions.plist" \
    -exportPath "$export_dir" \
    -allowProvisioningUpdates

  local exported_ipa="$export_dir/$scheme.ipa"
  [[ -f "$exported_ipa" ]] || {
    print -u2 "Xcode did not export $exported_ipa"
    exit 1
  }

  cp "$exported_ipa" "$package_dir/$ipa_name"
  unzip -p "$exported_ipa" 'Payload/*.app/Info.plist' \
    | /usr/bin/plutil -extract CFBundleVersion raw - \
    > "$package_dir/$ipa_name.version"
}

require_command xcodegen
require_command xcodebuild
require_command unzip

mkdir -p "$build_root/export" "$package_dir"
write_export_options
build_app ios/CooperTheCop CooperTheCop CooperTheCop.ipa
build_app ios/CooperWatch CooperWatch CooperWatch.ipa

print "OTA packages built under $package_dir"
