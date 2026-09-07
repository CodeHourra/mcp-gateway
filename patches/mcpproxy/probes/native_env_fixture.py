"""Stdio MCP fixture: report only whether native form environment values match."""
import argparse
import json
import os
from pathlib import Path
import sys
import time


# Public, disposable test markers, never production credentials.
EXPECTED = {
    "MCP_GATEWAY_NATIVE_ENV_ALPHA": "native-env-one",
    "MCP_GATEWAY_NATIVE_ENV_BETA": "native env two = with spaces",
    "MCP_GATEWAY_NATIVE_ENV_GAMMA": "原生环境变量三",
}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--evidence", type=Path)
    args = parser.parse_args()
    for line in sys.stdin:
        message = json.loads(line)
        if "id" not in message:
            continue
        method = message.get("method")
        if method == "initialize":
            result = {"protocolVersion": "2025-11-25", "capabilities": {"tools": {}},
                "serverInfo": {"name": "native-env-fixture", "version": "1"}}
        elif method == "tools/list":
            result = {"tools": [{"name": "check_env",
                "description": "Check three disposable native form environment values without returning their contents.",
                "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False},
                "annotations": {"readOnlyHint": True, "destructiveHint": False, "openWorldHint": False}}]}
        elif method == "tools/call":
            matches = {name: os.environ.get(name) == value for name, value in EXPECTED.items()}
            value = {"pid": os.getpid(), "expected_environment_matched": matches,
                "all_environment_matched": all(matches.values()),
                "management_key_not_inherited": not bool(os.environ.get("MCPPROXY_API_KEY"))}
            if args.evidence:
                args.evidence.parent.mkdir(parents=True, exist_ok=True)
                with args.evidence.open("a") as stream:
                    stream.write(json.dumps({"at": time.strftime("%Y-%m-%dT%H:%M:%S%z"), **value}) + "\n")
            result = {"content": [{"type": "text", "text": json.dumps(value)}],
                "structuredContent": value, "isError": False}
        else:
            result = {}
        print(json.dumps({"jsonrpc": "2.0", "id": message["id"], "result": result}), flush=True)


if __name__ == "__main__":
    main()
