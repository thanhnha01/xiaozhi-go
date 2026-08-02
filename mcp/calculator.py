# server.py
import sys
import logging
import os

# fastmcp 启动时会联网到 PyPI 检查新版本；若机器配置了 SOCKS 代理而未装
# socksio，httpx 会在启动阶段抛 ImportError 导致整个服务崩溃。
# 这里在导入 fastmcp 前关闭版本检查（也可用环境变量 FASTMCP_CHECK_FOR_UPDATES=off）。
os.environ.setdefault("FASTMCP_CHECK_FOR_UPDATES", "off")

from fastmcp import FastMCP

logger = logging.getLogger('Calculator')

# Fix UTF-8 encoding for Windows console
if sys.platform == 'win32':
    sys.stderr.reconfigure(encoding='utf-8')
    sys.stdout.reconfigure(encoding='utf-8')

import math
import random

# Create an MCP server
mcp = FastMCP("Calculator")

# Add an addition tool
@mcp.tool()
def calculator(python_expression: str) -> dict:
    """For mathamatical calculation, always use this tool to calculate the result of a python expression. You can use 'math' or 'random' directly, without 'import'."""
    result = eval(python_expression, {"math": math, "random": random})
    logger.info(f"Calculating formula: {python_expression}, result: {result}")
    return {"success": True, "result": result}

# Start the server
if __name__ == "__main__":
    mcp.run(transport="stdio")
