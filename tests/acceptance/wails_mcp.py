"""Call the debug-only Wails MCP API on the isolated local acceptance app."""
import argparse
import json
from pathlib import Path
import urllib.parse
import urllib.request


def call(endpoint, tool, arguments):
    parsed = urllib.parse.urlparse(endpoint)
    if parsed.scheme != "http" or parsed.hostname not in ("127.0.0.1", "::1"):
        raise ValueError("Acceptance endpoint must be HTTP on a loopback IP")
    request = urllib.request.Request(endpoint, data=json.dumps({"jsonrpc": "2.0", "id": 1,
        "method": "tools/call", "params": {"name": tool, "arguments": arguments}}).encode(),
        headers={"Content-Type": "application/json", "Accept": "application/json, text/event-stream"})
    with urllib.request.urlopen(request, timeout=60) as response:
        raw = response.read().decode()
    if raw.startswith("event:"):
        raw = next(line[6:] for line in raw.splitlines() if line.startswith("data: "))
    result = json.loads(raw)
    if result.get("error"):
        raise RuntimeError(result["error"])
    result = result["result"]
    if result.get("isError"):
        raise RuntimeError(result)
    content = result.get("content", [])
    if len(content) == 1 and content[0].get("type") == "text":
        try:
            return json.loads(content[0]["text"])
        except json.JSONDecodeError:
            return content[0]["text"]
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--endpoint", default="http://127.0.0.1:19199/mcp")
    parser.add_argument("--tool", required=True)
    parser.add_argument("--args", default="{}")
    parser.add_argument("--record")
    args = parser.parse_args()
    result = call(args.endpoint, args.tool, json.loads(args.args))
    encoded = json.dumps(result, ensure_ascii=False, indent=2) + "\n"
    if args.record:
        Path(args.record).write_text(encoded)
    print(encoded)
