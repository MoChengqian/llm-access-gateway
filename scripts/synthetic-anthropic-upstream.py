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


def utc_timestamp() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


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

    def _record_request(self, payload: dict) -> None:
        self.server.last_request = {
            "path": urlparse(self.path).path,
            "headers": {
                "x-api-key": self.headers.get("x-api-key", ""),
                "anthropic-version": self.headers.get("anthropic-version", ""),
                "accept": self.headers.get("Accept", ""),
                "content-type": self.headers.get("Content-Type", ""),
            },
            "payload": payload,
        }

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
                "data": [
                    {
                        "id": "claude-3-5-sonnet-latest",
                        "type": "model",
                        "created_at": utc_timestamp(),
                    }
                ]
            },
        )

    def _write_completion(self, model: str, text: str) -> None:
        self._write_json(
            200,
            {
                "id": f"msg-upstream-{next(REQUEST_IDS)}",
                "type": "message",
                "role": "assistant",
                "content": [{"type": "text", "text": text}],
                "model": model or "claude-3-5-sonnet-latest",
                "stop_reason": "end_turn",
                "usage": {
                    "input_tokens": 4,
                    "output_tokens": 3,
                },
            },
        )

    def _write_stream_ok(self, model: str) -> None:
        stream_id = f"msg-upstream-{next(REQUEST_IDS)}"
        chunks = [
            (
                "message_start",
                {
                    "type": "message_start",
                    "message": {
                        "id": stream_id,
                        "type": "message",
                        "role": "assistant",
                        "model": model or "claude-3-5-sonnet-latest",
                        "content": [],
                        "usage": {"input_tokens": 4, "output_tokens": 0},
                    },
                },
            ),
            (
                "content_block_delta",
                {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "text_delta", "text": "Synthetic "},
                },
            ),
            (
                "content_block_delta",
                {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "text_delta", "text": "Anthropic "},
                },
            ),
            (
                "message_delta",
                {
                    "type": "message_delta",
                    "delta": {"stop_reason": "end_turn"},
                },
            ),
            (
                "message_stop",
                {
                    "type": "message_stop",
                },
            ),
        ]

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.end_headers()
        for event_name, payload in chunks:
            self.wfile.write(f"event: {event_name}\n".encode("utf-8"))
            self.wfile.write(f"data: {json.dumps(payload)}\n\n".encode("utf-8"))
            self.wfile.flush()
            time.sleep(0.02)

    def _write_stream_partial(self, model: str) -> None:
        stream_id = f"msg-upstream-{next(REQUEST_IDS)}"
        events = [
            (
                "message_start",
                {
                    "type": "message_start",
                    "message": {
                        "id": stream_id,
                        "type": "message",
                        "role": "assistant",
                        "model": model or "claude-3-5-sonnet-latest",
                        "content": [],
                    },
                },
            ),
            (
                "content_block_delta",
                {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "text_delta", "text": "anthropic partial "},
                },
            ),
        ]

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.end_headers()
        for event_name, payload in events:
            self.wfile.write(f"event: {event_name}\n".encode("utf-8"))
            self.wfile.write(f"data: {json.dumps(payload)}\n\n".encode("utf-8"))
            self.wfile.flush()
            time.sleep(0.02)
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

        if path == "/debug/last-request":
            self._write_json(200, self.server.last_request)
            return

        if path == "/v1/models":
            self.log_message("models mode=%s", self.server.mode)
            self._write_models()
            return

        self._write_json(404, {"error": "not found"})

    def do_POST(self) -> None:
        if urlparse(self.path).path != "/v1/messages":
            self._write_json(404, {"error": "not found"})
            return

        payload = self._read_json()
        self._record_request(payload)

        model = payload.get("model", "claude-3-5-sonnet-latest")
        stream = bool(payload.get("stream"))
        mode = self.server.mode

        self.log_message("messages mode=%s stream=%s", mode, stream)

        if mode == "timeout":
            time.sleep(self.server.delay_seconds)

        if mode == "error500":
            self._write_json(
                500,
                {"type": "error", "error": {"type": "api_error", "message": "synthetic upstream 500"}},
            )
            return

        if mode == "stream_prechunk_fail" and stream:
            self._write_json(
                500,
                {"type": "error", "error": {"type": "api_error", "message": "synthetic anthropic prechunk failure"}},
            )
            return

        if mode == "stream_partial" and stream:
            self._write_stream_partial(model)
            return

        if stream:
            self._write_stream_ok(model)
            return

        if mode == "capture_system":
            messages = payload.get("messages", [])
            first_role = messages[0].get("role", "") if messages else ""
            text = "system=%s;messages=%d;first_role=%s" % (
                payload.get("system", ""),
                len(messages),
                first_role,
            )
            self._write_completion(model, text)
            return

        self._write_completion(model, "Synthetic Anthropic response.")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument(
        "--mode",
        choices=["ok", "capture_system", "timeout", "error500", "stream_prechunk_fail", "stream_partial"],
        required=True,
    )
    parser.add_argument("--delay-seconds", type=float, default=2.5)
    args = parser.parse_args()

    server = ThreadingHTTPServer((args.host, args.port), Handler)
    server.mode = args.mode
    server.delay_seconds = args.delay_seconds
    server.last_request = {}
    print(f"{now()} anthropic upstream listening host={args.host} port={args.port} mode={args.mode}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
