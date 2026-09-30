#!/bin/bash
# rotate-signing-identity.sh — replace the wimy-dev code-signing identity
# with a new one and give the same one to CI, without exporting anything
# from the keychain.
#
# 1. generates a new self-signed code-signing key + certificate in a
#    private temp dir;
# 2. stores it (as .p12 + password) in the `release` environment's
#    secrets of the GitHub repo, creating the environment with you as
#    required reviewer if it doesn't exist (GitHub side first: if that
#    fails, nothing changed);
# 3. deletes the old identity from the login keychain (two identities
#    with one name make codesign refuse), imports the new one and trusts
#    it for code signing (macOS asks for your password).
#
# The Accessibility permission belongs to the old certificate: approve
# Wimy once more after the next install. The temp dir is removed on exit.
#
#   ./rotate-signing-identity.sh [owner/repo]
set -euo pipefail

name=${WIMY_SIGN_ID:-wimy-dev}
repo=${1:-jaym/wimy}
keychain=$HOME/Library/Keychains/login.keychain-db
ssl=/usr/bin/openssl # LibreSSL: `security import` reads its PKCS#12

gh auth status >/dev/null

tmp=$(mktemp -d)
chmod 700 "$tmp"
trap 'rm -rf "$tmp"' EXIT

cat >"$tmp/openssl.cnf" <<CNF
[req]
distinguished_name = dn
prompt = no
x509_extensions = ext
[dn]
CN = $name
[ext]
basicConstraints = critical,CA:false
keyUsage = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
CNF
"$ssl" req -x509 -newkey rsa:2048 -nodes -days 3650 -config "$tmp/openssl.cnf" \
	-keyout "$tmp/key.pem" -out "$tmp/cert.pem" 2>/dev/null
pass=$("$ssl" rand -hex 24)
"$ssl" pkcs12 -export -inkey "$tmp/key.pem" -in "$tmp/cert.pem" -name "$name" \
	-out "$tmp/identity.p12" -passout "pass:$pass"
echo "generated a new \"$name\" ($("$ssl" x509 -in "$tmp/cert.pem" -noout -fingerprint -sha1 | cut -d= -f2))"

echo "GitHub: environment \"release\" and its signing secrets…"
# creating an environment needs the repo owner/an admin; reuse one that exists
if ! gh api "repos/$repo/environments/release" >/dev/null 2>&1; then
	gh api -X PUT "repos/$repo/environments/release" \
		-F "reviewers[][type]=User" -F "reviewers[][id]=$(gh api user -q .id)" >/dev/null
fi
if [ "$(gh api "repos/$repo/environments/release" -q '[.protection_rules[]? | select(.type == "required_reviewers")] | length')" = 0 ]; then
	echo "warning: \"release\" has no required reviewers: any tag pushed by someone with write access" >&2
	echo "         produces a signed build. Add yourself under Settings → Environments → release." >&2
fi
base64 -i "$tmp/identity.p12" | gh secret set WIMY_SIGNING_P12 --env release --repo "$repo"
printf '%s' "$pass" | gh secret set WIMY_SIGNING_P12_PASSWORD --env release --repo "$repo"

echo "keychain: replacing \"$name\"…"
for h in $(security find-certificate -a -c "$name" -Z "$keychain" 2>/dev/null | awk '/SHA-1 hash:/ {print $3}'); do
	security delete-identity -Z "$h" "$keychain" >/dev/null 2>&1 ||
		security delete-certificate -Z "$h" "$keychain" >/dev/null
done
security import "$tmp/identity.p12" -k "$keychain" -P "$pass" -T /usr/bin/codesign >/dev/null
echo "trusting it for code signing (macOS asks for your password)…"
security add-trusted-cert -r trustRoot -p codeSign -k "$keychain" "$tmp/cert.pem"

security find-identity -v -p codesigning | grep "\"$name\""
echo "done: local builds and the release workflow now sign with the same \"$name\""
