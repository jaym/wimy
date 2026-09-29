#!/usr/bin/env bash
# Linux CI: formatting, vet, unit tests, pure-Go build, e2e suites.
set -eu
cd "$(dirname "$0")"

unformatted=$(gofmt -l cmd internal | grep -v '^internal/proto/gen.go$' || true)
if [ -n "$unformatted" ]; then
  echo "gofmt needed:"; echo "$unformatted"; exit 1
fi
go vet ./...
go test ./...  # -race needs cgo; the darwin CI job runs it
CGO_ENABLED=0 go build ./...
./e2e-all.sh
