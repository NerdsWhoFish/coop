#!/bin/zsh
# End-to-end smoke test for the Coop backend.
set -e

BASE="http://localhost:${COOP_SMOKE_PORT:-8123}/api/v1"
PASS=0
FAIL=0

check() { # check <name> <actual> <expected>
  if [[ "$2" == "$3" ]]; then
    print "  ok   $1"
    PASS=$((PASS+1))
  else
    print "  FAIL $1: got '$2' want '$3'"
    FAIL=$((FAIL+1))
  fi
}

status() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

print "== setup status =="
NEEDS=$(curl -s "$BASE/setup" | jq -r .needsSetup)
check "fresh instance needs setup" "$NEEDS" "true"

print "== first-run setup =="
SETUP=$(curl -s -X POST "$BASE/setup" -H 'Content-Type: application/json' -d '{
  "familyName":"Test Family","timezone":"America/New_York",
  "email":"parent@example.com","password":"correct horse battery staple"}')
ADMIN_TOKEN=$(print "$SETUP" | jq -r .token)
check "setup returns a token" "$([[ -n $ADMIN_TOKEN && $ADMIN_TOKEN != null ]] && print yes)" "yes"
check "admin role" "$(print "$SETUP" | jq -r .parent.role)" "admin"

print "== setup is single-use =="
check "second setup rejected" \
  "$(status -X POST "$BASE/setup" -H 'Content-Type: application/json' -d '{
     "familyName":"Intruder","email":"bad@example.com","password":"correct horse battery staple"}')" \
  "409"

print "== auth =="
check "no token is 401" "$(status "$BASE/parent/me")" "401"
check "bad token is 401" "$(status "$BASE/parent/me" -H 'Authorization: Bearer nope')" "401"
check "good token is 200" "$(status "$BASE/parent/me" -H "Authorization: Bearer $ADMIN_TOKEN")" "200"

check "wrong password is 401" \
  "$(status -X POST "$BASE/parent/auth/login" -H 'Content-Type: application/json' \
     -d '{"email":"parent@example.com","password":"wrong password here"}')" "401"
check "unknown email is 401" \
  "$(status -X POST "$BASE/parent/auth/login" -H 'Content-Type: application/json' \
     -d '{"email":"nobody@example.com","password":"correct horse battery staple"}')" "401"

LOGIN=$(curl -s -X POST "$BASE/parent/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"parent@example.com","password":"correct horse battery staple"}')
check "login succeeds" "$(print "$LOGIN" | jq -r '.token != null')" "true"

print "== weak passwords rejected =="
check "short password rejected" \
  "$(status -X POST "$BASE/parent/parents" -H "Authorization: Bearer $ADMIN_TOKEN" \
     -H 'Content-Type: application/json' -d '{"email":"weak@example.com","password":"short"}')" "400"

print "== family =="
check "api key not configured yet" \
  "$(curl -s "$BASE/parent/family" -H "Authorization: Bearer $ADMIN_TOKEN" | jq -r .apiKeyConfigured)" "false"
check "quota endpoint lists three budgets" \
  "$(curl -s "$BASE/parent/family/quota" -H "Authorization: Bearer $ADMIN_TOKEN" | jq 'length')" "3"

print "== children =="
ALICE=$(curl -s -X POST "$BASE/parent/children" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Alice"}')
ALICE_ID=$(print "$ALICE" | jq -r .id)
check "child created" "$(print "$ALICE" | jq -r .name)" "Alice"
check "shorts on by default" "$(print "$ALICE" | jq -r .shortsEnabled)" "true"
check "watch autoplay off by default" "$(print "$ALICE" | jq -r .watchPageAutoplay)" "false"

BOB_ID=$(curl -s -X POST "$BASE/parent/children" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Bob"}' | jq -r .id)

check "admin sees both children" \
  "$(curl -s "$BASE/parent/children" -H "Authorization: Bearer $ADMIN_TOKEN" | jq 'length')" "2"

print "== child settings patch =="
curl -s -X PATCH "$BASE/parent/children/$ALICE_ID" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"shortsEnabled":false}' > /dev/null
check "shorts disabled" \
  "$(curl -s "$BASE/parent/children/$ALICE_ID" -H "Authorization: Bearer $ADMIN_TOKEN" | jq -r .shortsEnabled)" "false"
check "unrelated setting untouched" \
  "$(curl -s "$BASE/parent/children/$ALICE_ID" -H "Authorization: Bearer $ADMIN_TOKEN" | jq -r .videoSearchTiles)" "true"

print "== scoped parent =="
curl -s -X POST "$BASE/parent/parents" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"scoped@example.com\",\"password\":\"another good long password\",\"childIds\":[\"$ALICE_ID\"]}" > /dev/null

SCOPED_TOKEN=$(curl -s -X POST "$BASE/parent/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"scoped@example.com","password":"another good long password"}' | jq -r .token)

check "scoped parent sees only their child" \
  "$(curl -s "$BASE/parent/children" -H "Authorization: Bearer $SCOPED_TOKEN" | jq 'length')" "1"
check "scoped parent can read their child" \
  "$(status "$BASE/parent/children/$ALICE_ID" -H "Authorization: Bearer $SCOPED_TOKEN")" "200"
# 404 not 403: a 403 would confirm the other child exists.
check "other child is 404 not 403" \
  "$(status "$BASE/parent/children/$BOB_ID" -H "Authorization: Bearer $SCOPED_TOKEN")" "404"
check "scoped parent cannot create children" \
  "$(status -X POST "$BASE/parent/children" -H "Authorization: Bearer $SCOPED_TOKEN" \
     -H 'Content-Type: application/json' -d '{"name":"Sneaky"}')" "403"
check "scoped parent cannot list parents" \
  "$(status "$BASE/parent/parents" -H "Authorization: Bearer $SCOPED_TOKEN")" "403"

print "== last admin protected =="
ADMIN_ID=$(curl -s "$BASE/parent/me" -H "Authorization: Bearer $ADMIN_TOKEN" | jq -r .id)
check "cannot delete the only admin" \
  "$(status -X DELETE "$BASE/parent/parents/$ADMIN_ID" -H "Authorization: Bearer $ADMIN_TOKEN")" "409"

print "== pairing =="
PAIR=$(curl -s -X POST "$BASE/parent/children/$ALICE_ID/pairing-code" \
  -H "Authorization: Bearer $ADMIN_TOKEN")
CODE=$(print "$PAIR" | jq -r .code)
check "pairing code is grouped" "$(print "$CODE" | grep -c -- '-')" "1"
check "pairing url embeds public url" \
  "$(print "$PAIR" | jq -r .pairingUrl | grep -c 'coop.example')" "1"

check "bad pairing code rejected" \
  "$(status -X POST "$BASE/child/pair" -H 'Content-Type: application/json' \
     -d '{"code":"ZZZZ-ZZZZ","deviceName":"iPad"}')" "400"

# Lowercase and unseparated, to prove normalization works.
LOOSE=$(print "$CODE" | tr 'A-Z' 'a-z' | tr -d '-')
CHILD_TOKEN=$(curl -s -X POST "$BASE/child/pair" -H 'Content-Type: application/json' \
  -d "{\"code\":\"$LOOSE\",\"deviceName\":\"Alice iPad\"}" | jq -r .token)
check "sloppy code still pairs" "$([[ -n $CHILD_TOKEN && $CHILD_TOKEN != null ]] && print yes)" "yes"

check "code is single use" \
  "$(status -X POST "$BASE/child/pair" -H 'Content-Type: application/json' \
     -d "{\"code\":\"$CODE\",\"deviceName\":\"Second device\"}")" "400"

print "== child surface =="
check "child me works" "$(curl -s "$BASE/child/me" -H "Authorization: Bearer $CHILD_TOKEN" | jq -r .name)" "Alice"
check "child token rejected on parent surface" \
  "$(status "$BASE/parent/me" -H "Authorization: Bearer $CHILD_TOKEN")" "401"
check "parent token rejected on child surface" \
  "$(status "$BASE/child/me" -H "Authorization: Bearer $ADMIN_TOKEN")" "401"
check "empty feed is empty not an error" \
  "$(curl -s "$BASE/child/feed" -H "Authorization: Bearer $CHILD_TOKEN" | jq '.items | length')" "0"
# Alice has shorts disabled, so the tab must not exist for her.
check "shorts disabled is 404" \
  "$(status "$BASE/child/shorts" -H "Authorization: Bearer $CHILD_TOKEN")" "404"
check "device shows up for the parent" \
  "$(curl -s "$BASE/parent/children/$ALICE_ID/devices" -H "Authorization: Bearer $ADMIN_TOKEN" | jq 'length')" "1"

print "== device revocation =="
DEVICE_ID=$(curl -s "$BASE/parent/children/$ALICE_ID/devices" -H "Authorization: Bearer $ADMIN_TOKEN" | jq -r '.[0].id')
curl -s -X DELETE "$BASE/parent/devices/$DEVICE_ID" -H "Authorization: Bearer $ADMIN_TOKEN" > /dev/null
check "revoked token stops working" "$(status "$BASE/child/me" -H "Authorization: Bearer $CHILD_TOKEN")" "401"

print "== keywords =="
KW=$(curl -s -X POST "$BASE/parent/keywords" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"term":"scary"}')
check "keyword defaults to whole word" "$(print "$KW" | jq -r .wholeWord)" "true"
check "keyword defaults to title and tags" \
  "$(print "$KW" | jq -r '.matchTitle and .matchTags')" "true"
# Description matching false-positives hard, so it must default off.
check "description matching defaults off" "$(print "$KW" | jq -r .matchDescription)" "false"
check "scoped parent cannot create a family-wide keyword" \
  "$(status -X POST "$BASE/parent/keywords" -H "Authorization: Bearer $SCOPED_TOKEN" \
     -H 'Content-Type: application/json' -d '{"term":"anything"}')" "403"

print "== malformed input =="
check "unknown json field rejected" \
  "$(status -X POST "$BASE/parent/children" -H "Authorization: Bearer $ADMIN_TOKEN" \
     -H 'Content-Type: application/json' -d '{"name":"X","bogusField":1}')" "400"
check "malformed uuid rejected" \
  "$(status "$BASE/parent/children/not-a-uuid" -H "Authorization: Bearer $ADMIN_TOKEN")" "400"

print ""
print "passed: $PASS   failed: $FAIL"
[[ $FAIL -eq 0 ]]
