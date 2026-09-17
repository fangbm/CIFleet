#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 4 ]]; then
  echo "usage: $0 <output-dir> <controller-host-or-ip> <agent-id> <agent-host-or-ip>" >&2
  echo "example: $0 ./pki pi-controller.lan e5 e5.lan" >&2
  exit 2
fi

out=$1
controller_host=$2
agent_id=$3
agent_host=$4
mkdir -p "$out"
chmod 700 "$out"

san() {
  local value=$1
  if [[ $value =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || [[ $value == *:* ]]; then
    printf 'IP:%s' "$value"
  else
    printf 'DNS:%s' "$value"
  fi
}

openssl genrsa -out "$out/ca.key" 4096 >/dev/null 2>&1
openssl req -x509 -new -sha256 -days 3650 \
  -key "$out/ca.key" \
  -subj "/CN=CIFleet Local CA" \
  -out "$out/ca.crt"

issue_cert() {
  local name=$1
  local cn=$2
  local host=$3
  local ext="$out/$name.ext"

  openssl genrsa -out "$out/$name.key" 3072 >/dev/null 2>&1
  openssl req -new -sha256 \
    -key "$out/$name.key" \
    -subj "/CN=$cn" \
    -out "$out/$name.csr"

  cat > "$ext" <<EXT
subjectAltName=$(san "$host")
extendedKeyUsage=serverAuth,clientAuth
keyUsage=digitalSignature,keyEncipherment
EXT

  openssl x509 -req -sha256 -days 825 \
    -in "$out/$name.csr" \
    -CA "$out/ca.crt" \
    -CAkey "$out/ca.key" \
    -CAcreateserial \
    -extfile "$ext" \
    -out "$out/$name.crt" >/dev/null 2>&1

  rm -f "$out/$name.csr" "$ext"
  chmod 600 "$out/$name.key"
}

issue_cert controller controller "$controller_host"
issue_cert "$agent_id" "$agent_id" "$agent_host"
chmod 600 "$out/ca.key"
chmod 644 "$out/ca.crt" "$out/controller.crt" "$out/$agent_id.crt"

echo "PKI written to $out"
echo "controller certificate: CN=controller SAN=$(san "$controller_host")"
echo "agent certificate:      CN=$agent_id SAN=$(san "$agent_host")"
echo "Keep ca.key offline after provisioning."
