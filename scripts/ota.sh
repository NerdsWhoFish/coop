#!/bin/zsh
set -euo pipefail

usage() {
  print -u2 'usage: scripts/ota.sh <build|serve|stop|all>'
  exit 2
}

[[ $# -eq 1 ]] || usage
action="$1"
case "$action" in
  build|serve|stop|all) ;;
  *) usage ;;
esac

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
: "${COOP_OTA_HOST:?set COOP_OTA_HOST in deploy/ota/.env}"
COOP_OTA_HTTP_PORT="${COOP_OTA_HTTP_PORT:-8081}"
COOP_OTA_HTTPS_PORT="${COOP_OTA_HTTPS_PORT:-8444}"

build_root="$repo_root/.build/ota"
site_dir="$build_root/site"
cert_dir="$build_root/certs"
compose_file="$repo_root/deploy/ota/docker-compose.yml"
base_url="https://$COOP_OTA_HOST:$COOP_OTA_HTTPS_PORT"

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

write_manifest() {
  local output="$1"
  local ipa_name="$2"
  local bundle_id="$3"
  local bundle_version="$4"
  local title="$5"

  /usr/bin/plutil -create xml1 "$output"
  /usr/bin/plutil -insert items -array "$output"
  /usr/bin/plutil -insert items.0 -dictionary "$output"
  /usr/bin/plutil -insert items.0.assets -array "$output"
  /usr/bin/plutil -insert items.0.assets.0 -dictionary "$output"
  /usr/bin/plutil -insert items.0.assets.0.kind -string software-package "$output"
  /usr/bin/plutil -insert items.0.assets.0.url -string "$base_url/apps/$ipa_name" "$output"
  /usr/bin/plutil -insert items.0.metadata -dictionary "$output"
  /usr/bin/plutil -insert items.0.metadata.bundle-identifier -string "$bundle_id" "$output"
  /usr/bin/plutil -insert items.0.metadata.bundle-version -string "$bundle_version" "$output"
  /usr/bin/plutil -insert items.0.metadata.kind -string software "$output"
  /usr/bin/plutil -insert items.0.metadata.title -string "$title" "$output"
}

build_app() {
  local project_dir="$1"
  local scheme="$2"
  local ipa_name="$3"
  local bundle_id="$4"
  local title="$5"
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

  cp "$exported_ipa" "$site_dir/apps/$ipa_name"
  local bundle_version
  bundle_version="$(unzip -p "$exported_ipa" 'Payload/*.app/Info.plist' | /usr/bin/plutil -extract CFBundleVersion raw -)"
  write_manifest "$site_dir/manifests/${ipa_name%.ipa}.plist" "$ipa_name" "$bundle_id" "$bundle_version" "$title"
}

build_packages() {
  require_command xcodegen
  require_command xcodebuild
  require_command mkcert
  require_command unzip

  mkdir -p "$build_root/export" "$site_dir/apps" "$site_dir/manifests" "$cert_dir"
  cp "$repo_root/deploy/ota/index.html" "$site_dir/index.html"
  cp "$repo_root/deploy/ota/install.js" "$site_dir/install.js"
  cp "$(mkcert -CAROOT)/rootCA.pem" "$site_dir/ca.pem"
  print -r -- "window.coopOTAConfig = { httpPort: $COOP_OTA_HTTP_PORT, httpsPort: $COOP_OTA_HTTPS_PORT };" > "$site_dir/config.js"

  mkcert -cert-file "$cert_dir/server.pem" -key-file "$cert_dir/server-key.pem" "$COOP_OTA_HOST"
  write_export_options
  build_app ios/CooperTheCop CooperTheCop CooperTheCop.ipa fish.nerdswhofish.coop.parent 'Cooper The Cop'
  build_app ios/CooperWatch CooperWatch CooperWatch.ipa fish.nerdswhofish.coop.child 'Cooper Watch'

  /usr/bin/plutil -lint "$site_dir/manifests/CooperTheCop.plist" "$site_dir/manifests/CooperWatch.plist"
  print "OTA packages built under $site_dir"
}

serve_packages() {
  require_command docker
  require_command mkcert
  [[ -f "$site_dir/apps/CooperTheCop.ipa" && -f "$site_dir/apps/CooperWatch.ipa" ]] || {
    print -u2 'OTA packages are missing; run scripts/ota.sh build first'
    exit 1
  }
  [[ -f "$cert_dir/server.pem" && -f "$cert_dir/server-key.pem" ]] || {
    print -u2 'OTA certificate is missing; run scripts/ota.sh build first'
    exit 1
  }

  docker compose --project-directory "$repo_root/deploy/ota" -f "$compose_file" config --quiet
  docker compose --project-directory "$repo_root/deploy/ota" -f "$compose_file" up -d
  curl --fail --silent --show-error --cacert "$(mkcert -CAROOT)/rootCA.pem" "$base_url/" >/dev/null

  print "Certificate bootstrap: http://$COOP_OTA_HOST:$COOP_OTA_HTTP_PORT"
  print "Secure package bay:  $base_url"
}

stop_server() {
  require_command docker
  docker compose --project-directory "$repo_root/deploy/ota" -f "$compose_file" down
}

case "$action" in
  build) build_packages ;;
  serve) serve_packages ;;
  stop) stop_server ;;
  all)
    build_packages
    serve_packages
    ;;
esac
