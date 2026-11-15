#!/bin/bash
# Build completa: payload Linux (server sul cabinato), exe Windows, installer NSIS, zip.
set -e
cd "$(dirname "$0")"
V=$(grep -o '!define VERSION "[^"]*"' installer/setup.nsi | cut -d'"' -f2)
echo "versione $V"
mkdir -p dist cmd/companion/payload
rm -f cmd/companion/payload/companion-linux-amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$V" -o dist/companion-linux-amd64 ./cmd/companion
cp dist/companion-linux-amd64 cmd/companion/payload/companion-linux-amd64
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -s -w -X main.version=$V" -o dist/BatoceraCompanion.exe ./cmd/companion
(cd installer && makensis -V1 setup.nsi)
(cd dist && rm -f BatoceraCompanion-Setup-$V.zip && zip -q BatoceraCompanion-Setup-$V.zip BatoceraCompanion-Setup-$V.exe)
ls -la dist/BatoceraCompanion-Setup-$V.zip
