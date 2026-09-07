"""Controlled loopback listener owned by the native port-recovery test."""
import json,os,socket,sys
from pathlib import Path
with socket.socket() as listener:
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(('127.0.0.1',int(sys.argv[1])))
    listener.listen()
    record={'pid':os.getpid(),'port':listener.getsockname()[1],'scope':'controlled external port-occupancy sentinel'}
    Path(sys.argv[2]).write_text(json.dumps(record)+'\n')
    print(json.dumps(record),flush=True)
    while True:
        connection,_=listener.accept()
        with connection:
            connection.recv(2048)
            connection.sendall(b'HTTP/1.1 200 OK\r\nContent-Length: 8\r\nConnection: close\r\n\r\nsentinel')
