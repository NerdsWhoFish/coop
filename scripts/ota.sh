#!/bin/zsh
set -euo pipefail

usage() {
  print -u2 'usage: scripts/ota.sh build|publish'
  exit 2
}

[[ $# -eq 1 ]] || usage
action="$1"
[[ "$action" == build || "$action" == publish ]] || usage

repo_root="${0:A:h:h}"
config_file="$repo_root/deploy/ota/.env"
[[ -f "$config_file" ]] || {
  print -u2 'deploy/ota/.env is missing; copy deploy/ota/.env.example and configure it'
  exit 1
}

set -a
source "$config_file"
set +a

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

publish_packages() {
  : "${COOP_KUBE_CONTEXT:?set COOP_KUBE_CONTEXT in deploy/ota/.env}"
  : "${COOP_KUBE_NAMESPACE:?set COOP_KUBE_NAMESPACE in deploy/ota/.env}"
  : "${COOP_OTA_PVC:?set COOP_OTA_PVC in deploy/ota/.env}"
  local remote_directory="/var/lib/coop/ota"
  local upload_id="upload-$$"
  local claims
  local pod
  local file

  for file in CooperTheCop.ipa CooperTheCop.ipa.version CooperWatch.ipa CooperWatch.ipa.version; do
    [[ -f "$package_dir/$file" ]] || {
      print -u2 "$package_dir/$file is missing; run scripts/ota.sh build first"
      exit 1
    }
  done

  pod="$(kubectl --context "$COOP_KUBE_CONTEXT" -n "$COOP_KUBE_NAMESPACE" get pods \
    -l app.kubernetes.io/name=coop \
    -o jsonpath='{.items[0].metadata.name}')"
  [[ -n "$pod" ]] || {
    print -u2 "no Coop pod found in $COOP_KUBE_NAMESPACE"
    exit 1
  }
  claims="$(kubectl --context "$COOP_KUBE_CONTEXT" -n "$COOP_KUBE_NAMESPACE" get pod "$pod" \
    -o jsonpath='{.spec.volumes[*].persistentVolumeClaim.claimName}')"
  [[ " $claims " == *" $COOP_OTA_PVC "* ]] || {
    print -u2 "pod $pod does not mount PVC $COOP_OTA_PVC"
    exit 1
  }

  cleanup_upload() {
    kubectl --context "$COOP_KUBE_CONTEXT" -n "$COOP_KUBE_NAMESPACE" exec "$pod" -- \
      rm -rf "$remote_directory/$upload_id" >/dev/null 2>&1 || true
  }
  trap cleanup_upload EXIT INT TERM

  kubectl --context "$COOP_KUBE_CONTEXT" -n "$COOP_KUBE_NAMESPACE" exec "$pod" -- \
    sh -c 'test -d "$1" && mkdir "$1/$2"' sh "$remote_directory" "$upload_id"
  for file in CooperTheCop.ipa CooperTheCop.ipa.version CooperWatch.ipa CooperWatch.ipa.version; do
    kubectl --context "$COOP_KUBE_CONTEXT" -n "$COOP_KUBE_NAMESPACE" cp \
      "$package_dir/$file" "$pod:$remote_directory/$upload_id/$file"
  done
  kubectl --context "$COOP_KUBE_CONTEXT" -n "$COOP_KUBE_NAMESPACE" exec "$pod" -- \
    sh -c 'set -eu
      directory="$1"
      upload="$directory/$2"
      test -s "$upload/CooperTheCop.ipa"
      test -s "$upload/CooperTheCop.ipa.version"
      test -s "$upload/CooperWatch.ipa"
      test -s "$upload/CooperWatch.ipa.version"
      mv "$upload/CooperTheCop.ipa" "$directory/CooperTheCop.ipa"
      mv "$upload/CooperTheCop.ipa.version" "$directory/CooperTheCop.ipa.version"
      mv "$upload/CooperWatch.ipa" "$directory/CooperWatch.ipa"
      mv "$upload/CooperWatch.ipa.version" "$directory/CooperWatch.ipa.version"
      rmdir "$upload"' sh "$remote_directory" "$upload_id"
  trap - EXIT INT TERM

  print "Published OTA packages to PVC $COOP_OTA_PVC through pod $pod"
}

case "$action" in
  build)
    : "${COOP_DEVELOPMENT_TEAM:?set COOP_DEVELOPMENT_TEAM in deploy/ota/.env}"
    require_command xcodegen
    require_command xcodebuild
    require_command unzip
    mkdir -p "$build_root/export" "$package_dir"
    write_export_options
    build_app ios/CooperTheCop CooperTheCop CooperTheCop.ipa
    build_app ios/CooperWatch CooperWatch CooperWatch.ipa
    print "OTA packages built under $package_dir"
    ;;
  publish)
    require_command kubectl
    publish_packages
    ;;
esac
