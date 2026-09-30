#!/usr/bin/env bash
# update-release.sh vX.Y.Z — point the flake at a published release:
# fetches Wimy-X.Y.Z-arm64.zip, checks it against the release's .sha256
# and writes nix/release.json. Commit the result.
set -euo pipefail
cd "$(dirname "$0")/.."

tag=${1:?usage: nix/update-release.sh vX.Y.Z}
version=${tag#v}
url="https://github.com/jaym/wimy/releases/download/$tag/Wimy-$version-arm64.zip"

sri=$(nix store prefetch-file --json "$url" | jq -r .hash)
want=$(curl -fsSL "$url.sha256" | cut -d' ' -f1)
got=$(nix hash convert --hash-algo sha256 --to base16 "$sri")
if [ "$want" != "$got" ]; then
	echo "update-release: sha256 mismatch for $url: release says $want, downloaded $got" >&2
	exit 1
fi

cat >nix/release.json <<JSON
{
  "version": "$version",
  "url": "$url",
  "hash": "$sri"
}
JSON
echo "nix/release.json -> $tag ($sri)"
