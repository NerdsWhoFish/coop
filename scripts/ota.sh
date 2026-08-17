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
  local publisher_image="busybox:1.37.0@sha256:9db7b59979c38555a39def84a31fb98b5296952f9e3afd4f6f11f05b07adfab0"
  local remote_directory="/packages"
  local upload_id="upload-$$"
  local pod="coop-ota-publisher-$$"
  local file

  for file in CooperTheCop.ipa CooperTheCop.ipa.version CooperWatch.ipa CooperWatch.ipa.version; do
    [[ -f "$package_dir/$file" ]] || {
      print -u2 "$package_dir/$file is missing; run scripts/ota.sh build first"
      exit 1
    }
  done

  cleanup_publisher() {
    kubectl --context "$COOP_KUBE_CONTEXT" -n "$COOP_KUBE_NAMESPACE" delete pod "$pod" \
      --ignore-not-found --wait=false >/dev/null 2>&1 || true
  }
  trap cleanup_publisher EXIT INT TERM

  jq -n \
    --arg name "$pod" \
    --arg namespace "$COOP_KUBE_NAMESPACE" \
    --arg image "$publisher_image" \
    --arg claim "$COOP_OTA_PVC" \
    '{
      apiVersion: "v1",
      kind: "Pod",
      metadata: {name: $name, namespace: $namespace},
      spec: {
        restartPolicy: "Never",
        securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532},
        containers: [{
          name: "publisher",
          image: $image,
          command: ["sh", "-c", "sleep 3600"],
          securityContext: {
            allowPrivilegeEscalation: false,
            capabilities: {drop: ["ALL"]},
            readOnlyRootFilesystem: true
          },
          volumeMounts: [{name: "packages", mountPath: "/packages"}]
        }],
        volumes: [{name: "packages", persistentVolumeClaim: {claimName: $claim}}]
      }
    }' | kubectl --context "$COOP_KUBE_CONTEXT" apply -f -
  kubectl --context "$COOP_KUBE_CONTEXT" -n "$COOP_KUBE_NAMESPACE" wait \
    --for=condition=Ready "pod/$pod" --timeout=2m

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
  cleanup_publisher
  trap - EXIT INT TERM

  print "Published OTA packages to PVC $COOP_OTA_PVC"
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
    require_command jq
    publish_packages
    ;;
esac
