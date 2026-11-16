#!/bin/sh
set -eu
if [ ! -f /etc/ocserv/server-key.pem ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 2 -keyout /etc/ocserv/server-key.pem -out /etc/ocserv/server-cert.pem -subj /CN=localhost -addext subjectAltName=DNS:localhost,IP:127.0.0.1 2>/dev/null
  printf '%s\n%s\n' "$TEST_PASSWORD" "$TEST_PASSWORD" | ocpasswd -c /etc/ocserv/ocpasswd test
  sed -i "s/tcp-port = 443/tcp-port = $VPN_PORT/; s/udp-port = 443/udp-port = $VPN_PORT/" /etc/ocserv/ocserv.conf
fi
python3 /opt/test/services.py &
exec ocserv -f -d 1 -c /etc/ocserv/ocserv.conf
