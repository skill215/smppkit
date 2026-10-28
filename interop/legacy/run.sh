#!/bin/sh
# Build the optional peer harness in a temporary module. Neither checkout's
# go.mod/go.sum is edited, and the public library gains no legacy dependency.
set -eu
if [ "$#" -ne 1 ]; then
    echo "usage: sh interop/legacy/run.sh /path/to/go-smpp" >&2
    exit 2
fi
legacy_root=$(cd "$1" && pwd)
harness_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
library_root=$(cd "$harness_root/../.." && pwd)
test -f "$legacy_root/smpp/smpptest/server.go"
harness_tmp=$(mktemp -d "${TMPDIR:-/tmp}/smppkit-legacy.XXXXXXXX")
trap 'rm -rf "$harness_tmp"' EXIT HUP INT TERM
cp "$harness_root/legacy_test.go" "$harness_tmp/legacy_test.go"
# Preserve dependency checksums already checked into the reference checkout.
cp "$legacy_root/go.sum" "$harness_tmp/go.sum"
cd "$harness_tmp"
export GOWORK=off
# Local-only by default. Callers may explicitly configure a proxy if they need
# to populate dependency caches; the recorded run used no network downloads.
export GOPROXY=${GOPROXY:-off}
go mod init smppkit-legacy-check
go mod edit -go=1.26.0 -require=github.com/skill215/smppkit@v0.0.0 -require=github.com/skill215/go-smpp@v0.0.0
go mod edit "-replace=github.com/skill215/smppkit=$library_root" "-replace=github.com/skill215/go-smpp=$legacy_root"
go test -mod=mod -tags=legacyinterop -race -count=1 -timeout=30s -v ./...
