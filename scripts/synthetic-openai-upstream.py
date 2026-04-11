#!/usr/bin/env python3

import argparse
import json
import socket
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from itertools import count
from urllib.parse import urlparse


REQUEST_IDS = count(1)


def now() -> str:
    return time.strftime("%Y-%m-%d %H:%M:%S")


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt: str, *args) -> None:
        sys.stdout.write(f"{now()} {fmt % args}\n")
        sys.stdout.flush()

    def _read_json(self):
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length > 0 else b"{}"
        try:
            return json.loads(raw.decode("utf-8"))
        except Exception:
            return {}

    def _write_json(self, status: int, payload: dict) -> None:
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
        self.wfile.flush()

    def _write_models(self) -> None:
        self._write_json(
            200,
            {
                "object": "list",
                "data": [
                    {
                        "id": "gpt-4o-mini",
                        "object": "model",
                        "created": int(time.time()),
                        "owned_by": "lag-upstream",
                    }
                ],
            },
        )

    def _write_completion(self, model: str) -> None:
        self._write_json(
            200,
            {
                "id": f"chatcmpl-upstream-{next(REQUEST_IDS)}",
                "object": "chat.completion",
                "created": int(time.time()),
                "model": model or "gpt-4o-mini",
                "choices": [
                    {
                        "index": 0,
                        "message": {
                            "role": "assistant",
                            "content": "Synthetic upstream response.",
                        },
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": 4,
                    "completion_tokens": 3,
                    "total_tokens": 7,
                },
            },
        )

    def _write_stream_ok(self, model: str) -> None:
        chunks = [
            {
                "id": f"chatcmpl-upstream-{next(REQUEST_IDS)}",
                "object": "chat.completion.chunk",
                "created": int(time.time()),
                "model": model or "gpt-4o-mini",
                "choices": [
                    {
                        "index": 0,
                        "delta": {"role": "assistant", "content": "Synthetic "},
                        "finish_reason": "",
                    }
                ],
            },
            {
                "id": f"chatcmpl-upstream-{next(REQUEST_IDS)}",
                "object": "chat.completion.chunk",
                "created": int(time.time()),
                "model": model or "gpt-4o-mini",
                "choices": [
                    {
                        "index": 1,
                        "delta": {"role": "assistant", "content": "upstream "},
                        "finish_reason": "",
                    }
                ],
            },
            {
                "id": f"chatcmpl-upstream-{next(REQUEST_IDS)}",
                "object": "chat.completion.chunk",
                "created": int(time.time()),
                "model": model or "gpt-4o-mini",
                "choices": [
                    {
                        "index": 0,
                        "delta": {"role": "assistant"},
                        "finish_reason": "stop",
                    }
                ],
            },
        ]

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.end_headers()
        for chunk in chunks:
            self.wfile.write(f"data: {json.dumps(chunk)}\n\n".encode("utf-8"))
            self.wfile.flush()
            time.sleep(0.02)
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()

    def _write_stream_partial(self, model: str) -> None:
        chunk = {
            "id": f"chatcmpl-upstream-{next(REQUEST_IDS)}",
            "object": "chat.completion.chunk",
            "created": int(time.time()),
            "model": model or "gpt-4o-mini",
            "choices": [
                {
                    "index": 0,
                    "delta": {"role": "assistant", "content": "partial "},
                    "finish_reason": "",
                }
            ],
        }

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.end_headers()
        self.wfile.write(f"data: {json.dumps(chunk)}\n\n".encode("utf-8"))
        self.wfile.flush()
        time.sleep(0.05)
        try:
            self.connection.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
        try:
            self.connection.close()
        except OSError:
            pass

    def do_GET(self) -> None:
        path = urlparse(self.path).path

        if path == "/healthz":
            self._write_json(200, {"status": "ok", "mode": self.server.mode})
            return

        if path == "/v1/models":
            self.log_message("models mode=%s", self.server.mode)
            self._write_models()
            return
        self._write_json(404, {"error": "not found"})

    def do_POST(self) -> None:
        if urlparse(self.path).path != "/v1/chat/completions":
            self._write_json(404, {"error": "not found"})
            return

        payload = self._read_json()
        model = payload.get("model", "gpt-4o-mini")
        stream = bool(payload.get("stream"))
        mode = self.server.mode

        self.log_message("chat mode=%s stream=%s", mode, stream)

        if mode == "timeout":
            time.sleep(self.server.delay_seconds)

        if mode == "error500":
            self._write_json(500, {"error": {"message": "synthetic upstream 500"}})
            return

        if mode == "stream_prechunk_fail" and stream:
            self._write_json(500, {"error": {"message": "synthetic stream prechunk failure"}})
            return

        if mode == "stream_partial" and stream:
            self._write_stream_partial(model)
            return

        if stream:
            self._write_stream_ok(model)
            return

        self._write_completion(model)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument(
        "--mode",
        choices=["ok", "timeout", "error500", "stream_prechunk_fail", "stream_partial"],
        required=True,
    )
    parser.add_argument("--delay-seconds", type=float, default=2.5)
    args = parser.parse_args()

    # Local CI-only synthetic upstream; TLS is intentionally terminated outside this loopback test server. # NOSONAR
    server = ThreadingHTTPServer((args.host, args.port), Handler)
    server.mode = args.mode
    server.delay_seconds = args.delay_seconds
    print(f"{now()} upstream listening host={args.host} port={args.port} mode={args.mode}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
