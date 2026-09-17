from __future__ import annotations

import argparse
import hashlib
import json
import sys
import threading
import urllib.error
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

_CACHE_LOCK = threading.Lock()
_REASONING_BY_CALL_ID: dict[str, str] = {}
_LAST_REASONING_BY_SESSION: dict[str, str] = {}
_MAX_REASONING_ENTRIES = 2000


def _cache_reasoning(call_id: str, reasoning: str) -> None:
    if not call_id:
        return
    with _CACHE_LOCK:
        if len(_REASONING_BY_CALL_ID) >= _MAX_REASONING_ENTRIES:
            oldest_keys = list(_REASONING_BY_CALL_ID.keys())[:200]
            for k in oldest_keys:
                _REASONING_BY_CALL_ID.pop(k, None)
        _REASONING_BY_CALL_ID[call_id] = reasoning


def _get_reasoning_for_call(call_id: str) -> str:
    with _CACHE_LOCK:
        return _REASONING_BY_CALL_ID.get(call_id, "")


def _cache_session_reasoning(session_id: str, reasoning: str) -> None:
    if not session_id:
        return
    with _CACHE_LOCK:
        if len(_LAST_REASONING_BY_SESSION) >= _MAX_REASONING_ENTRIES:
            oldest_keys = list(_LAST_REASONING_BY_SESSION.keys())[:200]
            for k in oldest_keys:
                _LAST_REASONING_BY_SESSION.pop(k, None)
        _LAST_REASONING_BY_SESSION[session_id] = reasoning


def _get_session_reasoning(session_id: str) -> str:
    with _CACHE_LOCK:
        return _LAST_REASONING_BY_SESSION.get(session_id, "")


def _id(prefix: str) -> str:
    return f"{prefix}_{uuid.uuid4().hex}"


def _text_content(content: Any) -> str:
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return ""
    parts: list[str] = []
    for part in content:
        if not isinstance(part, dict):
            continue
        if part.get("type") in {"input_text", "output_text"}:
            parts.append(str(part.get("text") or ""))
    return "\n".join(part for part in parts if part)


def _convert_input(
    request: dict[str, Any],
    session_id: str = "",
) -> list[dict[str, Any]]:
    messages: list[dict[str, Any]] = []
    instructions = str(request.get("instructions") or "")
    if instructions:
        messages.append({"role": "system", "content": instructions})

    pending_tool_calls: list[dict[str, Any]] = []

    def flush_tool_calls() -> None:
        if pending_tool_calls:
            reasoning = ""
            for call in pending_tool_calls:
                cid = call.get("id")
                if cid:
                    found = _get_reasoning_for_call(cid)
                    if found:
                        reasoning = found
                        break
            if not reasoning and session_id:
                reasoning = _get_session_reasoning(session_id)

            if (
                messages
                and messages[-1].get("role") == "assistant"
                and "tool_calls" not in messages[-1]
            ):
                messages[-1]["tool_calls"] = list(pending_tool_calls)
                messages[-1]["reasoning_content"] = reasoning
            else:
                messages.append(
                    {
                        "role": "assistant",
                        "content": None,
                        "reasoning_content": reasoning,
                        "tool_calls": list(pending_tool_calls),
                    }
                )
            pending_tool_calls.clear()

    raw_input = request.get("input")
    if isinstance(raw_input, str):
        messages.append({"role": "user", "content": raw_input})
        return messages
    if not isinstance(raw_input, list):
        return messages

    for item in raw_input:
        if not isinstance(item, dict):
            continue
        item_type = str(item.get("type") or "")
        if item_type == "message":
            flush_tool_calls()
            role = str(item.get("role") or "user")
            if role == "developer":
                role = "system"
            messages.append({"role": role, "content": _text_content(item.get("content"))})
        elif item_type in {"function_call", "custom_tool_call"}:
            namespace = str(item.get("namespace") or "")
            tool_name = (
                f"{namespace}__{item.get('name') or ''}"
                if namespace
                else str(item.get("name") or "")
            )
            arguments = item.get("arguments")
            if arguments is None:
                arguments = json.dumps({"input": str(item.get("input") or "")})
            pending_tool_calls.append(
                {
                    "id": str(item.get("call_id") or item.get("id") or _id("call")),
                    "type": "function",
                    "function": {
                        "name": tool_name,
                        "arguments": str(arguments),
                    },
                }
            )
        elif item_type in {"function_call_output", "custom_tool_call_output"}:
            flush_tool_calls()
            messages.append(
                {
                    "role": "tool",
                    "tool_call_id": str(item.get("call_id") or ""),
                    "content": str(item.get("output") or ""),
                }
            )
    flush_tool_calls()
    return messages


def _convert_tools(
    raw_tools: Any,
) -> tuple[list[dict[str, Any]], dict[str, str]]:
    tools: list[dict[str, Any]] = []
    tool_kinds: dict[str, str] = {}
    seen_names: set[str] = set()
    if not isinstance(raw_tools, list):
        return tools, tool_kinds

    def add_tool(tool: Any, namespace: str = "") -> None:
        if not isinstance(tool, dict):
            return
        tool_type = str(tool.get("type") or "")
        if tool_type == "namespace":
            namespace_name = str(tool.get("name") or "")
            nested_tools = tool.get("tools")
            if isinstance(nested_tools, list):
                for nested_tool in nested_tools:
                    add_tool(nested_tool, namespace_name)
            return
        name = str(tool.get("name") or "")
        wire_name = f"{namespace}__{name}" if namespace else name
        if not name or wire_name in seen_names:
            return
        if tool_type == "function":
            parameters = tool.get("parameters")
            if not isinstance(parameters, dict):
                parameters = {"type": "object", "properties": {}}
            tool_kinds[wire_name] = "function"
        elif tool_type == "custom":
            parameters = {
                "type": "object",
                "properties": {"input": {"type": "string"}},
                "required": ["input"],
            }
            tool_kinds[wire_name] = "custom"
        else:
            return
        tools.append(
            {
                "type": "function",
                "function": {
                    "name": wire_name,
                    "description": str(tool.get("description") or ""),
                    "parameters": parameters,
                },
            }
        )
        seen_names.add(wire_name)

    for tool in raw_tools:
        add_tool(tool)
    return tools, tool_kinds


def _chat_request(
    request: dict[str, Any],
    session_id: str = "",
) -> tuple[dict[str, Any], dict[str, str]]:
    tools, tool_kinds = _convert_tools(request.get("tools"))
    chat: dict[str, Any] = {
        "model": str(request.get("model") or ""),
        "messages": _convert_input(request, session_id=session_id),
        "stream": False,
    }
    if tools:
        chat["tools"] = tools
        tool_choice = request.get("tool_choice")
        if isinstance(tool_choice, str):
            chat["tool_choice"] = tool_choice
    if request.get("max_output_tokens") is not None:
        chat["max_tokens"] = request["max_output_tokens"]
    for key in ("temperature", "top_p", "parallel_tool_calls"):
        if request.get(key) is not None:
            chat[key] = request[key]
    text = request.get("text")
    if isinstance(text, dict) and isinstance(text.get("format"), dict):
        response_format = text["format"]
        if response_format.get("type") == "json_schema":
            chat["response_format"] = {
                "type": "json_schema",
                "json_schema": {
                    "name": response_format.get("name") or "response",
                    "strict": bool(response_format.get("strict")),
                    "schema": response_format.get("schema") or {},
                },
            }
    return chat, tool_kinds


def _response(
    chat_response: dict[str, Any],
    requested_model: str,
    tool_kinds: dict[str, str],
    session_id: str = "",
) -> dict[str, Any]:
    choices = chat_response.get("choices")
    choice = choices[0] if isinstance(choices, list) and choices else {}
    message = choice.get("message") if isinstance(choice, dict) else {}
    if not isinstance(message, dict):
        message = {}

    reasoning_content = str(message.get("reasoning_content") or "")
    if session_id and reasoning_content:
        _cache_session_reasoning(session_id, reasoning_content)

    output: list[dict[str, Any]] = []
    content = message.get("content")
    if isinstance(content, str) and content:
        output.append(
            {
                "id": _id("msg"),
                "type": "message",
                "status": "completed",
                "role": "assistant",
                "content": [
                    {
                        "type": "output_text",
                        "text": content,
                        "annotations": [],
                        "logprobs": [],
                    }
                ],
            }
        )

    raw_tool_calls = message.get("tool_calls")
    if isinstance(raw_tool_calls, list):
        for raw_call in raw_tool_calls:
            if not isinstance(raw_call, dict):
                continue
            function = raw_call.get("function")
            if not isinstance(function, dict):
                continue
            wire_name = str(function.get("name") or "")
            namespace = ""
            name = wire_name
            if wire_name.startswith("mcp__") and "__" in wire_name[5:]:
                namespace_prefix, name = wire_name.rsplit("__", 1)
                namespace = namespace_prefix
            arguments = str(function.get("arguments") or "{}")
            call_id = str(raw_call.get("id") or _id("call"))
            if reasoning_content:
                _cache_reasoning(call_id, reasoning_content)
            if tool_kinds.get(wire_name) == "custom":
                try:
                    decoded = json.loads(arguments)
                    tool_input = (
                        str(decoded.get("input") or "")
                        if isinstance(decoded, dict)
                        else arguments
                    )
                except json.JSONDecodeError:
                    tool_input = arguments
                item = {
                    "id": _id("ctc"),
                    "type": "custom_tool_call",
                    "status": "completed",
                    "call_id": call_id,
                    "name": name,
                    "input": tool_input,
                }
            else:
                item = {
                    "id": _id("fc"),
                    "type": "function_call",
                    "status": "completed",
                    "call_id": call_id,
                    "name": name,
                    "arguments": arguments,
                }
            if namespace:
                item["namespace"] = namespace
            output.append(item)

    usage = chat_response.get("usage")
    if not isinstance(usage, dict):
        usage = {}
    completion_details = usage.get("completion_tokens_details")
    if not isinstance(completion_details, dict):
        completion_details = {}
    input_tokens = int(usage.get("prompt_tokens") or 0)
    output_tokens = int(usage.get("completion_tokens") or 0)
    return {
        "id": _id("resp"),
        "object": "response",
        "created_at": int(chat_response.get("created") or 0),
        "status": "completed",
        "error": None,
        "incomplete_details": None,
        "model": str(chat_response.get("model") or requested_model),
        "output": output,
        "parallel_tool_calls": True,
        "usage": {
            "input_tokens": input_tokens,
            "input_tokens_details": {"cached_tokens": 0},
            "output_tokens": output_tokens,
            "output_tokens_details": {
                "reasoning_tokens": int(completion_details.get("reasoning_tokens") or 0)
            },
            "total_tokens": int(usage.get("total_tokens") or input_tokens + output_tokens),
        },
    }


def _events(response: dict[str, Any]) -> list[dict[str, Any]]:
    sequence = 0
    events: list[dict[str, Any]] = []

    def add(event: dict[str, Any]) -> None:
        nonlocal sequence
        event["sequence_number"] = sequence
        sequence += 1
        events.append(event)

    initial = dict(response)
    initial["status"] = "in_progress"
    initial["output"] = []
    add({"type": "response.created", "response": initial})
    add({"type": "response.in_progress", "response": initial})

    for output_index, completed_item in enumerate(response["output"]):
        item = dict(completed_item)
        item["status"] = "in_progress"
        if item["type"] == "message":
            item["content"] = []
        elif item["type"] == "function_call":
            item["arguments"] = ""
        elif item["type"] == "custom_tool_call":
            item["input"] = ""
        add(
            {
                "type": "response.output_item.added",
                "output_index": output_index,
                "item": item,
            }
        )
        item_id = completed_item["id"]
        if completed_item["type"] == "message":
            content = completed_item["content"][0]
            add(
                {
                    "type": "response.content_part.added",
                    "item_id": item_id,
                    "output_index": output_index,
                    "content_index": 0,
                    "part": {**content, "text": ""},
                }
            )
            add(
                {
                    "type": "response.output_text.delta",
                    "item_id": item_id,
                    "output_index": output_index,
                    "content_index": 0,
                    "delta": content["text"],
                }
            )
            add(
                {
                    "type": "response.output_text.done",
                    "item_id": item_id,
                    "output_index": output_index,
                    "content_index": 0,
                    "text": content["text"],
                    "logprobs": [],
                }
            )
            add(
                {
                    "type": "response.content_part.done",
                    "item_id": item_id,
                    "output_index": output_index,
                    "content_index": 0,
                    "part": content,
                }
            )
        elif completed_item["type"] == "function_call":
            add(
                {
                    "type": "response.function_call_arguments.delta",
                    "item_id": item_id,
                    "output_index": output_index,
                    "delta": completed_item["arguments"],
                }
            )
            add(
                {
                    "type": "response.function_call_arguments.done",
                    "item_id": item_id,
                    "output_index": output_index,
                    "name": completed_item["name"],
                    "arguments": completed_item["arguments"],
                }
            )
        elif completed_item["type"] == "custom_tool_call":
            add(
                {
                    "type": "response.custom_tool_call_input.delta",
                    "item_id": item_id,
                    "output_index": output_index,
                    "delta": completed_item["input"],
                }
            )
            add(
                {
                    "type": "response.custom_tool_call_input.done",
                    "item_id": item_id,
                    "output_index": output_index,
                    "input": completed_item["input"],
                }
            )
        add(
            {
                "type": "response.output_item.done",
                "output_index": output_index,
                "item": completed_item,
            }
        )
    add({"type": "response.completed", "response": response})
    return events


class BridgeHandler(BaseHTTPRequestHandler):
    server_version = "CommandCodeResponsesBridge/1.0"

    def _json(self, status: int, payload: dict[str, Any]) -> None:
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:
        if self.path.rstrip("/") == "/health":
            self._json(200, {"status": "ok"})
            return
        self._json(404, {"error": "not found"})

    def do_POST(self) -> None:
        if self.path not in {"/responses", "/v1/responses"}:
            self._json(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length") or "0")
            request = json.loads(self.rfile.read(length))
            if not isinstance(request, dict):
                raise ValueError("request must be an object")

            metadata = request.get("metadata") if isinstance(request.get("metadata"), dict) else {}
            session_id = (
                self.headers.get("x-opencode-session")
                or self.headers.get("x-codex-session-id")
                or self.headers.get("x-session-id")
                or self.headers.get("session-id")
                or self.headers.get("x-conversation-id")
                or self.headers.get("conversation-id")
                or metadata.get("session_id")
                or metadata.get("conversation_id")
                or metadata.get("chat_id")
                or request.get("session_id")
                or request.get("conversation_id")
            )
            if not session_id:
                raw_input = request.get("input")
                if isinstance(raw_input, list) and raw_input:
                    first = raw_input[0] if isinstance(raw_input[0], dict) else {}
                    item_id = str(first.get("id") or first.get("call_id") or "")
                    if item_id:
                        session_id = f"codex-{item_id}"
                    else:
                        first_txt = _text_content(first.get("content"))
                        if first_txt:
                            h = hashlib.sha256(first_txt.encode("utf-8")).hexdigest()[:16]
                            session_id = f"codex-{h}"
                if not session_id:
                    session_id = _id("codex_session")

            chat, tool_kinds = _chat_request(request, session_id=session_id)

            client_ua = self.headers.get("User-Agent") or ""
            upstream_ua = (
                client_ua
                if client_ua and "Python" not in client_ua and "urllib" not in client_ua
                else "codex/0.154.0"
            )

            upstream_headers = {
                "Authorization": self.headers.get("Authorization") or "",
                "Content-Type": "application/json",
                "User-Agent": upstream_ua,
                "x-opencode-session": str(session_id),
            }
            for header_name in ("x-codex-session-id", "x-session-id", "x-conversation-id"):
                header_val = self.headers.get(header_name)
                if header_val:
                    upstream_headers[header_name] = header_val

            upstream = urllib.request.Request(
                self.server.backend_url,
                data=json.dumps(chat).encode(),
                headers=upstream_headers,
                method="POST",
            )
            with urllib.request.urlopen(upstream, timeout=self.server.timeout) as result:
                chat_response = json.loads(result.read())
            response = _response(
                chat_response,
                str(request.get("model") or ""),
                tool_kinds,
                session_id=session_id,
            )
            if request.get("stream"):
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.send_header("Cache-Control", "no-cache")
                self.end_headers()
                for event in _events(response):
                    self.wfile.write(f"data: {json.dumps(event)}\n\n".encode())
                    self.wfile.flush()
                self.wfile.write(b"data: [DONE]\n\n")
                self.wfile.flush()
            else:
                self._json(200, response)
        except urllib.error.HTTPError as error:
            body = error.read()
            self.send_response(error.code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        except Exception as error:
            self._json(502, {"error": str(error)})

    def log_message(self, format: str, *args: Any) -> None:
        return


class BridgeServer(ThreadingHTTPServer):
    def __init__(self, address: tuple[str, int], backend_url: str, timeout: float):
        super().__init__(address, BridgeHandler)
        self.backend_url = backend_url
        self.timeout = timeout


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", required=True)
    parser.add_argument("--port", type=int, default=18088)
    parser.add_argument("--timeout", type=float, default=300)
    args = parser.parse_args()
    BridgeServer(("127.0.0.1", args.port), args.url, args.timeout).serve_forever()
    return 0


if __name__ == "__main__":
    sys.exit(main())
