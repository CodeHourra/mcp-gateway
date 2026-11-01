"""Local stdio MCP server for the README screenshots. No network, no credentials."""
import json, sys

TOOLS = [
    {"name": "echo", "description": "原样返回传入的文本，用于验证网关连通性", "inputSchema": {"type": "object", "properties": {"text": {"type": "string", "description": "要回显的文本"}}, "required": ["text"]}, "annotations": {"readOnlyHint": True}},
    {"name": "sum", "description": "计算一组整数的总和", "inputSchema": {"type": "object", "properties": {"numbers": {"type": "array", "items": {"type": "integer"}, "description": "整数列表"}}, "required": ["numbers"]}, "annotations": {"readOnlyHint": True}},
    {"name": "now", "description": "返回当前时间（ISO 8601）", "inputSchema": {"type": "object", "properties": {}}, "annotations": {"readOnlyHint": True}},
]

def reply(msg):
    method, mid = msg.get("method"), msg.get("id")
    if mid is None:
        return None
    if method == "initialize":
        result = {"protocolVersion": "2025-11-25", "capabilities": {"tools": {}}, "serverInfo": {"name": "demo-echo", "version": "1.0.0"}}
    elif method == "tools/list":
        result = {"tools": TOOLS}
    elif method == "tools/call":
        name = msg["params"]["name"]; args = msg["params"].get("arguments") or {}
        if name == "echo":
            result = {"content": [{"type": "text", "text": args.get("text", "")}], "structuredContent": {"echo": args.get("text", "")}}
        elif name == "sum":
            total = sum(args.get("numbers") or [])
            result = {"content": [{"type": "text", "text": str(total)}], "structuredContent": {"sum": total}}
        else:
            import datetime
            now = datetime.datetime.now().isoformat(timespec="seconds")
            result = {"content": [{"type": "text", "text": now}], "structuredContent": {"now": now}}
    else:
        result = {}
    return {"jsonrpc": "2.0", "id": mid, "result": result}

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    out = reply(json.loads(line))
    if out:
        sys.stdout.write(json.dumps(out) + "\n"); sys.stdout.flush()
