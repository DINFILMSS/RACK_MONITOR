#!/usr/bin/env bash
# Assina um .exe com Authenticode (osslsigncode) quando os secrets do certificado existem.
# Sem certificado, só avisa e segue (o Windows mostra o aviso do SmartScreen).
set -euo pipefail
f="$1"
if [ -z "${WINDOWS_CERT_PFX:-}" ]; then
  echo "::warning::$f sem assinatura de código (configure WINDOWS_CERT_PFX e WINDOWS_CERT_PASSWORD)."
  exit 0
fi
command -v osslsigncode >/dev/null || { sudo apt-get update -qq && sudo apt-get install -y -qq osslsigncode; }
pfx="$(mktemp)"; trap 'rm -f "$pfx" "$f.signed"' EXIT
echo "$WINDOWS_CERT_PFX" | base64 -d > "$pfx"
osslsigncode sign -pkcs12 "$pfx" -pass "${WINDOWS_CERT_PASSWORD:-}" \
  -n "RACK MONITOR BY DON" -h sha256 -ts http://timestamp.digicert.com \
  -in "$f" -out "$f.signed"
mv "$f.signed" "$f"
osslsigncode verify -in "$f" >/dev/null && echo "assinado: $f"
