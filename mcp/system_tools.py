"""
macOS system tools MCP server: open applications and open websites.
支持打开指定 App、打开指定网站，仅适用于 macOS。

Tools:
  - list_installed_apps : list apps installed on this Mac
  - open_app            : open a specific application
  - open_website        : open a website in the default browser
"""
import sys
import logging
import subprocess
import os

# fastmcp 启动时会联网到 PyPI 检查新版本；若机器配置了 SOCKS 代理而未装
# socksio，httpx 会在启动阶段抛 ImportError 导致整个服务崩溃。
# 这里在导入 fastmcp 前关闭版本检查（也可用环境变量 FASTMCP_CHECK_FOR_UPDATES=off）。
os.environ.setdefault("FASTMCP_CHECK_FOR_UPDATES", "off")

from fastmcp import FastMCP

logger = logging.getLogger('SystemTools')

# Fix UTF-8 encoding for Windows console
if sys.platform == 'win32':
    sys.stderr.reconfigure(encoding='utf-8')
    sys.stdout.reconfigure(encoding='utf-8')

# Create an MCP server
mcp = FastMCP("SystemTools")

# Common locations where apps live on macOS
APP_SEARCH_DIRS = [
    '/Applications',
    '/System/Applications',
    os.path.expanduser('~/Applications'),
]


def _all_search_dirs():
    """Return the app search dirs plus their Utilities subdirectories."""
    dirs = []
    for base in APP_SEARCH_DIRS:
        dirs.append(base)
        utils = os.path.join(base, 'Utilities')
        if os.path.isdir(utils):
            dirs.append(utils)
    return dirs


def _find_app(app_name: str):
    """Search common app directories for an .app matching app_name (case-insensitive)."""
    target = app_name.lower()
    for base in _all_search_dirs():
        if not os.path.isdir(base):
            continue
        try:
            entries = os.listdir(base)
        except OSError:
            continue
        for entry in entries:
            if entry.lower() == target or entry.lower() == target + '.app':
                path = os.path.join(base, entry)
                if os.path.isdir(path):
                    return path
    return None


@mcp.tool()
def list_installed_apps(keyword: str = "") -> dict:
    """List the applications installed on this macOS machine.

    Use this tool when:
    - You need to know which applications are available to open.
    - The user asks to open an app but you are not sure of its exact name
      (e.g. 'WeChat' vs 'Weixin', or a Chinese display name like 微信).
    - open_app failed and you want to find the correct app name.

    Args:
        keyword: optional filter; only apps whose name contains this text
                 (case-insensitive) are returned. Empty string lists all.

    Returns:
        dict: {"success": true, "count": N, "apps": [{"name": ..., "path": ...}, ...]}
    """
    if sys.platform != 'darwin':
        return {"success": False, "error": "list_installed_apps only works on macOS"}

    apps = []
    seen = set()
    for base in _all_search_dirs():
        if not os.path.isdir(base):
            continue
        try:
            entries = os.listdir(base)
        except OSError:
            continue
        for entry in entries:
            if not entry.endswith('.app'):
                continue
            app_name = entry[:-4]  # strip '.app'
            if app_name in seen:
                continue
            seen.add(app_name)
            if keyword and keyword.lower() not in app_name.lower():
                continue
            apps.append({"name": app_name, "path": os.path.join(base, entry)})

    apps.sort(key=lambda a: a['name'].lower())
    logger.info(f"Listed {len(apps)} apps (keyword={keyword or 'all'})")
    return {"success": True, "count": len(apps), "apps": apps}


@mcp.tool()
def open_app(app_name: str) -> dict:
    """Open a specific application on macOS (this machine). Supports Chinese display names.

    Use this tool when the user asks to open / launch / start an application,
    e.g. '打开微信', 'open Safari', '帮我启动 VS Code'.

    Args:
        app_name: the application to open. Acceptable forms, tried in order:
            - display name or file name, e.g. 'Safari', '微信', 'Visual Studio Code'
            - bundle identifier, e.g. 'com.apple.Safari'
            - absolute path to the .app bundle, e.g. '/Applications/Safari.app'

    Note:
        - If you are unsure whether the app is installed or what its exact
          name is, call list_installed_apps first to find the correct name.
        - If this tool fails, verify the app name with list_installed_apps and retry.

    Returns:
        dict: {"success": true, "method": "by-name|by-bundle-id|by-path", "app": ...}
    """
    if sys.platform != 'darwin':
        return {"success": False, "error": "open_app only works on macOS"}

    app_name = app_name.strip().strip('"').strip("'")
    if not app_name:
        return {"success": False, "error": "app_name must not be empty"}

    # 1) Open by display name / file name: `open -a "微信"`
    result = subprocess.run(
        ['open', '-a', app_name], capture_output=True, text=True, timeout=15
    )
    if result.returncode == 0:
        logger.info(f"Opened app by name: {app_name}")
        return {"success": True, "method": "by-name", "app": app_name}

    # 2) Open by bundle id: `open -b com.apple.Safari`
    result = subprocess.run(
        ['open', '-b', app_name], capture_output=True, text=True, timeout=15
    )
    if result.returncode == 0:
        logger.info(f"Opened app by bundle id: {app_name}")
        return {"success": True, "method": "by-bundle-id", "app": app_name}

    # 3) Search the filesystem for a matching .app and open by full path
    path = _find_app(app_name)
    if path:
        result = subprocess.run(
            ['open', path], capture_output=True, text=True, timeout=15
        )
        if result.returncode == 0:
            logger.info(f"Opened app by path: {path}")
            return {"success": True, "method": "by-path", "app": app_name, "path": path}

    # All attempts failed - give the AI a clear hint on what to do next
    logger.warning(f"Could not open app: {app_name}")
    return {
        "success": False,
        "error": f"Could not open app '{app_name}'. It may not be installed.",
        "hint": (
            "Call list_installed_apps to see which applications are installed "
            "on this machine, then retry with the exact name."
        ),
    }


@mcp.tool()
def open_website(url: str) -> dict:
    """Open a website in the default browser on macOS.

    Use this tool when the user asks to open / visit / go to a website or webpage,
    e.g. '打开百度', 'open GitHub', '帮我查一下天气预报网站'.

    Args:
        url: the website address, e.g. 'www.baidu.com' or 'https://github.com'.
             - If the user gives a site name in natural language (e.g. 百度, B站),
               convert it to its domain first, e.g. 'www.baidu.com', 'www.bilibili.com'.
             - A scheme (http:// or https://) is added automatically if missing.

    Returns:
        dict: {"success": true, "opened": "https://..."}
    """
    if sys.platform != 'darwin':
        return {"success": False, "error": "open_website only works on macOS"}

    url = url.strip().strip('"').strip("'")
    if not url:
        return {"success": False, "error": "url must not be empty"}

    # Reject obviously invalid input (injection / whitespace)
    if ' ' in url or '\n' in url or '\t' in url:
        return {"success": False, "error": f"invalid url: {url}"}

    # Add a scheme if missing
    if not url.startswith(('http://', 'https://')):
        url = 'https://' + url

    result = subprocess.run(['open', url], capture_output=True, text=True, timeout=15)
    if result.returncode == 0:
        logger.info(f"Opened website: {url}")
        return {"success": True, "opened": url}
    logger.warning(f"Failed to open website: {url}, {result.stderr}")
    return {"success": False, "error": result.stderr or f"open failed (code {result.returncode})"}


# Start the server
if __name__ == "__main__":
    mcp.run(transport="stdio")
