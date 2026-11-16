import socketserver
import struct
import threading


class TCP(socketserver.BaseRequestHandler):
    def handle(self):
        while data := self.request.recv(65535):
            self.request.sendall(data)


class UDP(socketserver.BaseRequestHandler):
    def handle(self):
        data, sock = self.request
        sock.sendto(data, self.client_address)


class DNS(socketserver.BaseRequestHandler):
    def handle(self):
        query, sock = self.request
        end = 12
        while query[end]:
            end += query[end] + 1
        end += 5
        answer = query[:2] + struct.pack('!5H', 0x8180, 1, 1, 0, 0)
        answer += query[12:end] + b'\xc0\x0c' + struct.pack('!HHIH', 1, 1, 30, 4) + bytes([192, 0, 2, 42])
        sock.sendto(answer, self.client_address)


socketserver.ThreadingTCPServer.allow_reuse_address = True
servers = [socketserver.ThreadingTCPServer(('0.0.0.0', 8000), TCP),
           socketserver.ThreadingUDPServer(('0.0.0.0', 8001), UDP),
           socketserver.ThreadingUDPServer(('0.0.0.0', 5353), DNS)]
for server in servers:
    threading.Thread(target=server.serve_forever, daemon=True).start()
threading.Event().wait()
