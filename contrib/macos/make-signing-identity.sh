#!/bin/bash
# make-signing-identity.sh — create the code-signing identity wimy's
# builds are signed with (run once).
#
# macOS ties the Accessibility permission to an app's code signature.
# Ad-hoc signatures (what `go build` produces) change with every build,
# so every update would silently lose the permission. A self-signed
# certificate gives every build the same identity: approve Wimy once,
# and updates keep working.
#
# This creates a certificate named wimy-dev (or $1) for code signing
# only, valid for 10 years, in your login keychain, and marks it
# trusted for code signing — macOS asks for your password for that.
#
#   ./make-signing-identity.sh [name]
set -euo pipefail

name=${1:-wimy-dev}
keychain=$HOME/Library/Keychains/login.keychain-db

if security find-identity -v -p codesigning | grep -q "\"$name\""; then
	echo "code-signing identity \"$name\" already exists"
	exit 0
fi

tmp=$(mktemp -d)
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

# macOS's own LibreSSL: `security import` can't read PKCS#12 files that
# OpenSSL 3 (e.g. from Nix) writes by default.
ssl=/usr/bin/openssl
"$ssl" req -x509 -newkey rsa:2048 -nodes -days 3650 -config "$tmp/openssl.cnf" \
	-keyout "$tmp/key.pem" -out "$tmp/cert.pem" 2>/dev/null
pass=$("$ssl" rand -hex 16)
"$ssl" pkcs12 -export -inkey "$tmp/key.pem" -in "$tmp/cert.pem" -name "$name" \
	-out "$tmp/identity.p12" -passout "pass:$pass"

security import "$tmp/identity.p12" -k "$keychain" -P "$pass" -T /usr/bin/codesign
echo "trusting \"$name\" for code signing (macOS asks for your password)…"
security add-trusted-cert -r trustRoot -p codeSign -k "$keychain" "$tmp/cert.pem"

security find-identity -v -p codesigning | grep "\"$name\"" && echo "done: make mac-app signs with \"$name\""
