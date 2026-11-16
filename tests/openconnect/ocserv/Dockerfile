FROM debian:trixie-slim@sha256:cedb1ef40439206b673ee8b33a46a03a0c9fa90bf3732f54704f99cb061d2c5a
RUN apt-get update && apt-get install -y --no-install-recommends ocserv=1.3.0-2 openssl python3 iproute2 iptables && rm -rf /var/lib/apt/lists/*
COPY ocserv.conf /etc/ocserv/ocserv.conf
COPY services.py entrypoint.sh /opt/test/
ENTRYPOINT ["/bin/sh", "/opt/test/entrypoint.sh"]
