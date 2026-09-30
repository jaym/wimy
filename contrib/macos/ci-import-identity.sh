#!/bin/bash
# ci-import-identity.sh — make the wimy-dev signing identity available
# to codesign on a CI runner (GitHub Actions, macOS).
#
# Reads the identity from the environment:
#   WIMY_SIGNING_P12           the .p12 file, base64-encoded
#   WIMY_SIGNING_P12_PASSWORD  its password
# and imports it into a throwaway keychain that codesign may use
# without prompting. `ci-import-identity.sh cleanup` deletes the
# keychain again (run it in an always() step).
set -euo pipefail

keychain=$RUNNER_TEMP/wimy-signing.keychain-db

if [ "${1:-}" = cleanup ]; then
	security delete-keychain "$keychain" 2>/dev/null || true
	rm -f "$RUNNER_TEMP/wimy-signing.p12" "$RUNNER_TEMP/wimy-signing.pem"
	exit 0
fi

: "${WIMY_SIGNING_P12:?missing (a release-environment secret)}"
: "${WIMY_SIGNING_P12_PASSWORD:?missing (a release-environment secret)}"

kcpass=$(openssl rand -hex 24)
p12=$RUNNER_TEMP/wimy-signing.p12
pem=$RUNNER_TEMP/wimy-signing.pem
printf '%s' "$WIMY_SIGNING_P12" | base64 --decode >"$p12"

security create-keychain -p "$kcpass" "$keychain"
security set-keychain-settings -lut 3600 "$keychain"
security unlock-keychain -p "$kcpass" "$keychain"
security import "$p12" -k "$keychain" -P "$WIMY_SIGNING_P12_PASSWORD" -T /usr/bin/codesign
# let codesign use the key without a GUI prompt
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$kcpass" "$keychain" >/dev/null
# search the new keychain too (keeping the others; paths may hold spaces)
existing=()
while IFS= read -r k; do
	k=${k#*\"}
	existing+=("${k%\"*}")
done < <(security list-keychains -d user)
security list-keychains -d user -s "$keychain" "${existing[@]}"

# the certificate is self-signed: trust it for code signing, or
# find-identity doesn't list it as valid (the runner has passwordless sudo)
/usr/bin/openssl pkcs12 -in "$p12" -passin "pass:$WIMY_SIGNING_P12_PASSWORD" -nokeys -out "$pem"
sudo security add-trusted-cert -d -r trustRoot -p codeSign -k /Library/Keychains/System.keychain "$pem"
rm -f "$p12" "$pem"

security find-identity -v -p codesigning "$keychain"
