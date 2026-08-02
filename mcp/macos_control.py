"""
macOS Control MCP Server  —  全面的 macOS 系统自动化工具
============================================================
为 AI 提供 28 个工具，覆盖 12 大类，让 AI 真正帮你管理 Mac。
所有工具仅使用 macOS 内置命令，无需额外安装依赖。

Categories / 分类:
  1.  Clipboard       剪贴板读写
  2.  Notifications   系统通知
  3.  System Info     系统信息（CPU、内存、磁盘、电池）
  4.  App Management  应用管理（安装、运行、启动、激活、退出、隐藏）
  5.  Process Mgmt    进程管理（搜索、终止）
  6.  Audio           音量控制
  7.  Power/Display   电源与显示（锁屏、休眠、熄屏）
  8.  Screenshots     截图
  9.  Finder/Desktop  访达与桌面（显示文件、清空废纸篓、换壁纸）
  10. Network         网络信息
  11. Search          Spotlight 搜索
  12. Shell           执行命令行

每个工具的描述都详细告诉 AI：是什么、何时用、参数怎么填、返回什么格式。
"""
import sys
import os

# fastmcp 会在启动时联网 PyPI 检查新版本，若系统配置了 SOCKS 代理而未装 socksio，
# httpx 会在启动阶段抛 ImportError 导致整个服务崩溃。必须在 import fastmcp 前关闭。
os.environ.setdefault("FASTMCP_CHECK_FOR_UPDATES", "off")

import logging
import subprocess
import shutil
import socket
import re
import time
import tempfile
from pathlib import Path

from fastmcp import FastMCP

logger = logging.getLogger("MacOSControl")

# Fix UTF-8 for Windows console (defensive)
if sys.platform == "win32":
    sys.stderr.reconfigure(encoding="utf-8")
    sys.stdout.reconfigure(encoding="utf-8")

mcp = FastMCP("MacOSControl")

# ═══════════════════════════════════════════════════════════════════════════════
# Shared Helpers / 共享辅助函数
# ═══════════════════════════════════════════════════════════════════════════════

APP_SEARCH_DIRS = [
    "/Applications",
    "/System/Applications",
    os.path.expanduser("~/Applications"),
]

MACOS_ONLY = {"success": False, "error": "此工具仅支持 macOS / macOS only"}

# AppleScript 转义：防止注入
def _ase(s: str) -> str:
    """Escape a string for safe embedding in an AppleScript double-quoted string."""
    return s.replace("\\", "\\\\").replace('"', '\\"')


def _run(cmd: list[str], timeout: float = 15) -> subprocess.CompletedProcess:
    """Run a command with unified settings: UTF-8, text mode, timeout."""
    return subprocess.run(
        cmd, capture_output=True, text=True, encoding="utf-8",
        errors="replace", timeout=timeout,
    )


def _osascript(script: str, timeout: float = 15) -> subprocess.CompletedProcess:
    """Run an AppleScript snippet. Multi-line scripts are piped via stdin."""
    if "\n" in script:
        return subprocess.run(
            ["osascript"], input=script, capture_output=True,
            text=True, encoding="utf-8", errors="replace", timeout=timeout,
        )
    return _run(["osascript", "-e", script], timeout=timeout)


def _darwin() -> dict | None:
    """Return error dict if not on macOS, else None."""
    if sys.platform != "darwin":
        return MACOS_ONLY
    return None


def _all_search_dirs() -> list[str]:
    """Return app search dirs plus their Utilities subdirectories."""
    dirs = []
    for base in APP_SEARCH_DIRS:
        dirs.append(base)
        utils = os.path.join(base, "Utilities")
        if os.path.isdir(utils):
            dirs.append(utils)
    return dirs


def _find_app(app_name: str) -> str | None:
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
            if entry.lower() == target or entry.lower() == target + ".app":
                path = os.path.join(base, entry)
                if os.path.isdir(path):
                    return path
    return None


def _parse_size_to_gb(total_kb: int) -> str:
    """Convert KB to human-readable GB string with 1 decimal."""
    return f"{total_kb / 1024 / 1024:.1f} GB"

# ═══════════════════════════════════════════════════════════════════════════════
# 1. Clipboard / 剪贴板
# ═══════════════════════════════════════════════════════════════════════════════

@mcp.tool()
def get_clipboard() -> dict:
    """Read the current text content of the macOS clipboard.

    Use this tool when:
    - You need to know what text is currently copied / cut on this Mac.
    - The user asks "what's on my clipboard" / "剪贴板里有什么".

    Note: This only reads plain text. Images, files, and rich text on the
    clipboard cannot be read and will return an empty string.

    Returns:
        {"success": True, "text": "clipboard content..."}
        {"success": True, "text": ""}  — clipboard is empty or non-text
    """
    if (e := _darwin()): return e
    try:
        r = _run(["pbpaste"])
        text = r.stdout
        return {"success": True, "text": text}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def set_clipboard(text: str) -> dict:
    """Write text to the macOS clipboard (pbcopy).

    Use this tool when:
    - The user asks you to copy something to clipboard.
    - You generate content and the user wants to paste it somewhere else.
    - Example: "把这个结果复制到剪贴板" / "copy this to clipboard".

    Args:
        text: the text to copy to clipboard.

    Returns:
        {"success": True}
    """
    if (e := _darwin()): return e
    try:
        _run(["pbcopy"], timeout=5)
        return {"success": False, "error": "pbcopy returned output unexpectedly"}
    except Exception:
        pass
    try:
        proc = subprocess.Popen(
            ["pbcopy"], stdin=subprocess.PIPE, text=True, encoding="utf-8",
        )
        proc.communicate(input=text, timeout=10)
        if proc.returncode == 0:
            return {"success": True}
        return {"success": False, "error": f"pbcopy exited with code {proc.returncode}"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


# ═══════════════════════════════════════════════════════════════════════════════
# 2. Notifications / 系统通知
# ═══════════════════════════════════════════════════════════════════════════════

VALID_SOUNDS = {
    "default", "Glass", "Basso", "Blow", "Bottle", "Frog",
    "Funk", "Hero", "Morse", "Ping", "Pop", "Purr", "Sosumi",
    "Submarine", "Tink",
}


@mcp.tool()
def send_notification(title: str, message: str, sound: str = "default") -> dict:
    """Send a native macOS notification banner.

    Use this tool to alert the user about:
    - A long-running operation has completed ("下载完成").
    - Important reminders or alerts.
    - Confirmation that an action was taken ("文件已保存").

    The notification appears as a banner in the top-right corner and in
    Notification Center. It does NOT block the user's workflow.

    Args:
        title:   notification title, keep it short (< 50 chars).
        message: notification body text.
        sound:   sound name. Available options:
                 "default", "Glass", "Basso", "Blow", "Bottle", "Frog",
                 "Funk", "Hero", "Morse", "Ping", "Pop", "Purr",
                 "Sosumi", "Submarine", "Tink".
                 Use "default" for the system default notification sound.

    Returns:
        {"success": True}
    """
    if (e := _darwin()): return e
    if sound not in VALID_SOUNDS:
        sound = "default"
    script = (
        f'display notification "{_ase(message)}"'
        f' with title "{_ase(title)}"'
        f' sound name "{_ase(sound)}"'
    )
    try:
        r = _osascript(script)
        if r.returncode == 0:
            logger.info(f"Sent notification: {title}")
            return {"success": True}
        return {"success": False, "error": r.stderr.strip() or "osascript failed"}
    except subprocess.TimeoutExpired:
        return {"success": False, "error": "发送通知超时 / notification timed out"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


# ═══════════════════════════════════════════════════════════════════════════════
# 3. System Information / 系统信息
# ═══════════════════════════════════════════════════════════════════════════════

@mcp.tool()
def get_system_info() -> dict:
    """Get comprehensive system information about this Mac.

    Returns: CPU model, chip, CPU cores, RAM (GB), macOS version,
    hostname, architecture, uptime, and memory pressure.

    Use this tool when:
    - The user asks about their Mac's specs ("我的电脑配置怎么样").
    - You need to know the machine's capabilities before suggesting actions.
    - The user asks about uptime, OS version, or hardware info.

    Returns:
        {"success": True, "cpu": {...}, "memory": {...}, "os": {...}, ...}
    """
    if (e := _darwin()): return e

    info = {}

    # CPU
    try:
        r = _run(["sysctl", "-n", "machdep.cpu.brand_string"])
        info["cpu_model"] = r.stdout.strip()
    except Exception:
        info["cpu_model"] = None

    try:
        r = _run(["sysctl", "-n", "hw.model"])
        info["model_identifier"] = r.stdout.strip()
    except Exception:
        info["model_identifier"] = None

    try:
        r = _run(["sysctl", "-n", "hw.ncpu"])
        info["cpu_cores"] = int(r.stdout.strip())
        r2 = _run(["sysctl", "-n", "hw.perflevel0.logicalcpu"])
        perf_cores = int(r2.stdout.strip())
        r3 = _run(["sysctl", "-n", "hw.perflevel1.logicalcpu"])
        eff_cores = int(r3.stdout.strip())
        info["cpu_cores_detail"] = f"{perf_cores}P+{eff_cores}E" if perf_cores and eff_cores else None
    except Exception:
        info["cpu_cores"] = None
        info["cpu_cores_detail"] = None

    # RAM
    try:
        r = _run(["sysctl", "-n", "hw.memsize"])
        bytes_ram = int(r.stdout.strip())
        info["ram_gb"] = round(bytes_ram / (1024 ** 3), 1)
    except Exception:
        info["ram_gb"] = None

    # OS
    try:
        r = _run(["sw_vers"])
        for line in r.stdout.strip().splitlines():
            if "ProductName" in line:
                info["os_name"] = line.split(":")[-1].strip()
            elif "ProductVersion" in line:
                info["os_version"] = line.split(":")[-1].strip()
            elif "BuildVersion" in line:
                info["os_build"] = line.split(":")[-1].strip()
    except Exception:
        info.setdefault("os_name", None)
        info.setdefault("os_version", None)

    # Architecture
    try:
        r = _run(["uname", "-m"])
        info["architecture"] = r.stdout.strip()
    except Exception:
        info["architecture"] = None

    # Hostname
    try:
        info["hostname"] = socket.gethostname()
    except Exception:
        info["hostname"] = None

    # Uptime
    try:
        r = _run(["sysctl", "-n", "kern.boottime"])
        m = re.search(r"sec\s*=\s*(\d+)", r.stdout)
        if m:
            boot_sec = int(m.group(1))
            uptime_sec = int(time.time() - boot_sec)
            days, rem = divmod(uptime_sec, 86400)
            hours, rem = divmod(rem, 3600)
            mins = rem // 60
            info["uptime_seconds"] = uptime_sec
            info["uptime_display"] = f"{days}d {hours}h {mins}m" if days else f"{hours}h {mins}m"
    except Exception:
        info["uptime_seconds"] = None

    # Memory pressure
    try:
        r = _run(["vm_stat"])
        page_size = 16384  # default for Apple Silicon
        ps_match = re.search(r"page size of (\d+) bytes", r.stdout)
        if ps_match:
            page_size = int(ps_match.group(1))
        stats = {}
        for line in r.stdout.strip().splitlines():
            m = re.match(r"(.+?):\s+(\d+)\.?", line)
            if m:
                stats[m.group(1).strip()] = int(m.group(2))
        free_pages = stats.get("Pages free", 0)
        speculative = stats.get("Pages speculative", 0)
        total_pages = sum(v for k, v in stats.items() if not k.endswith(":"))
        used_pages = total_pages - free_pages - speculative
        info["memory_pressure"] = {
            "page_size_bytes": page_size,
            "free_gb": round(free_pages * page_size / (1024 ** 3), 2),
            "used_gb": round(used_pages * page_size / (1024 ** 3), 2),
        }
    except Exception:
        info["memory_pressure"] = None

    logger.info(f"System info: {info.get('cpu_model')}, {info.get('ram_gb')}GB, {info.get('os_version')}")
    return {"success": True, **info}


@mcp.tool()
def get_disk_usage() -> dict:
    """Get disk usage information for all mounted volumes on this Mac.

    Use this tool when:
    - The user asks about disk space ("我还有多少磁盘空间").
    - You need to check if there's enough space before downloading or saving files.
    - The user asks about storage / 硬盘 / 存储.

    Returns total, used, and free space in GB for each volume, plus percentage used.
    Skips pseudo-filesystems and temporary mounts.

    Returns:
        {"success": True, "volumes": [{"name": "Macintosh HD", "mountpoint": "/",
         "total_gb": 500.0, "used_gb": 320.5, "free_gb": 179.5, "percent": 64.1}, ...]}
    """
    if (e := _darwin()): return e
    volumes = []
    try:
        r = _run(["df", "-k"], timeout=10)
        lines = r.stdout.strip().splitlines()
        if len(lines) < 2:
            return {"success": True, "volumes": []}

        for line in lines[1:]:
            parts = line.split()
            if len(parts) < 6:
                continue
            mountpoint = parts[-1]
            # Skip pseudo-filesystems
            if mountpoint.startswith("/dev") or "map " in line or mountpoint == "/private/var/filters":
                continue
            if mountpoint.startswith("/private/var/folders/"):
                continue

            try:
                total_kb = int(parts[1])
                used_kb = int(parts[2])
                free_kb = int(parts[3])
            except (ValueError, IndexError):
                continue

            total_gb = round(total_kb / 1024 / 1024, 1)
            used_gb = round(used_kb / 1024 / 1024, 1)
            free_gb = round(free_kb / 1024 / 1024, 1)
            percent = round(used_kb / total_kb * 100, 1) if total_kb > 0 else 0

            name = os.path.basename(mountpoint) if mountpoint != "/" else "Macintosh HD"
            if mountpoint == "/System/Volumes/Data":
                name = "Macintosh HD - Data"

            volumes.append({
                "name": name,
                "mountpoint": mountpoint,
                "total_gb": total_gb,
                "used_gb": used_gb,
                "free_gb": free_gb,
                "percent": percent,
            })

        logger.info(f"Disk usage: {len(volumes)} volumes")
        return {"success": True, "volumes": volumes}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def get_battery_status() -> dict:
    """Get battery status for this Mac laptop.

    Returns battery percentage, charging state, power source,
    cycle count, and health condition.

    Use this tool when:
    - The user asks about battery level ("电量还有多少").
    - The user asks whether the Mac is charging.
    - You want to warn the user about low battery.

    Note: On desktop Macs (Mac mini, Mac Studio, etc.), this will return
    battery_present = false.

    Returns:
        {"success": True, "battery_present": True/False, "level_pct": 85,
         "power_source": "AC Power", "charging": True, ...}
    """
    if (e := _darwin()): return e
    try:
        r = _run(["pmset", "-g", "batt"], timeout=10)
        output = r.stdout.strip()
        if "No battery" in output or "no battery" in output.lower():
            return {"success": True, "battery_present": False}

        result: dict = {"battery_present": True}

        # Parse percentage
        pct_match = re.search(r"(\d+)%", output)
        if pct_match:
            result["level_pct"] = int(pct_match.group(1))

        # Parse power source
        source_match = re.search(r"drawing from '([^']+)'", output)
        if source_match:
            result["power_source"] = source_match.group(1)

        # Parse charging state
        if "charging" in output.lower():
            if "not charging" in output.lower():
                result["charging"] = False
                result["state"] = "charged"  # AC attached, battery full
            elif "discharging" in output.lower():
                result["charging"] = False
                result["state"] = "discharging"
            else:
                result["charging"] = True
                result["state"] = "charging"
        elif "discharging" in output.lower():
            result["charging"] = False
            result["state"] = "discharging"

        # Cycle count and health (from system_profiler)
        try:
            r2 = _run(["system_profiler", "SPPowerDataType"], timeout=20)
            sp = r2.stdout
            cc = re.search(r"Cycle Count:\s*(\d+)", sp)
            if cc:
                result["cycle_count"] = int(cc.group(1))
            cond = re.search(r"Condition:\s*(\w+)", sp)
            if cond:
                result["battery_health"] = cond.group(1)
            max_cap = re.search(r"Maximum Capacity:\s*(\d+)%", sp)
            if max_cap:
                result["max_capacity_pct"] = int(max_cap.group(1))
        except Exception:
            pass

        logger.info(f"Battery: {result.get('level_pct')}%, {result.get('state')}")
        return {"success": True, **result}
    except Exception as ex:
        return {"success": False, "error": str(ex)}

# ═══════════════════════════════════════════════════════════════════════════════
# 4. Application Management / 应用管理
# ═══════════════════════════════════════════════════════════════════════════════

@mcp.tool()
def list_installed_apps(keyword: str = "") -> dict:
    """List the applications installed on this macOS machine.

    Use this tool when:
    - You need to know which applications are available to open.
    - The user asks to open an app but you are not sure of its exact name
      (e.g. "WeChat" vs "Weixin", or a Chinese display name like 微信).
    - open_app failed and you want to find the correct app name.
    - The user asks "what apps do I have installed" / "我装了哪些应用".

    Args:
        keyword: optional filter; only apps whose name contains this text
                 (case-insensitive) are returned. Empty string lists all.

    Returns:
        {"success": True, "count": N, "apps": [{"name": ..., "path": ...}, ...]}
    """
    if (e := _darwin()): return e
    apps = []
    seen: set[str] = set()
    for base in _all_search_dirs():
        if not os.path.isdir(base):
            continue
        try:
            entries = os.listdir(base)
        except OSError:
            continue
        for entry in entries:
            if not entry.endswith(".app"):
                continue
            app_name = entry[:-4]
            if app_name in seen:
                continue
            seen.add(app_name)
            if keyword and keyword.lower() not in app_name.lower():
                continue
            apps.append({"name": app_name, "path": os.path.join(base, entry)})

    apps.sort(key=lambda a: a["name"].lower())
    logger.info(f"Listed {len(apps)} apps (keyword={keyword or 'all'})")
    return {"success": True, "count": len(apps), "apps": apps}


@mcp.tool()
def list_running_apps() -> dict:
    """List all currently running processes/ apps on this Mac with their PIDs.

    Returns both foreground (Dock-visible) and background (menu bar, system)
    processes. Use list_installed_apps if you only want installed .app bundles.

    Use this tool when:
    - The user asks "what's running" / "开了哪些应用" / "当前运行的程序".
    - You need to find the PID of an app before activating or quitting it.
    - You want to check if a specific app is already running.

    Returns:
        {"success": True, "count": N,
         "apps": [{"name": "Safari", "pid": 1234, "foreground": True}, ...]}
    """
    if (e := _darwin()): return e
    try:
        # Get all processes with name, pid, and foreground status
        script = (
            'tell application "System Events"\n'
            'set output to ""\n'
            'repeat with p in every process\n'
            'set is_fg to not (background only of p)\n'
            'set output to output & name of p & "||" & unix id of p & "||" & is_fg & linefeed\n'
            'end repeat\n'
            'return text 1 thru -2 of output\n'
            'end tell'
        )
        r = _osascript(script, timeout=10)
        if r.returncode != 0:
            return {"success": False, "error": r.stderr.strip() or "AppleScript failed"}

        apps = []
        for line in r.stdout.strip().splitlines():
            line = line.strip()
            parts = line.split("||")
            if len(parts) >= 2:
                try:
                    apps.append({
                        "name": parts[0].strip(),
                        "pid": int(parts[1].strip()),
                        "foreground": parts[2].strip() == "true" if len(parts) >= 3 else None,
                    })
                except ValueError:
                    continue

        # Sort: foreground apps first
        apps.sort(key=lambda a: (not a["foreground"], a["name"].lower()))
        logger.info(f"Running apps: {len(apps)}")
        return {"success": True, "count": len(apps), "apps": apps}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def get_frontmost_app() -> dict:
    """Get the name and PID of the currently focused (frontmost) application.

    Use this tool when:
    - The user asks "what app am I in right now" / "我现在在用哪个应用".
    - You need context about what the user is currently doing before
      taking further actions.

    Returns:
        {"success": True, "app": {"name": "Safari", "pid": 1234}}
    """
    if (e := _darwin()): return e
    try:
        script = (
            'tell application "System Events"\n'
            'set p to first process whose frontmost is true\n'
            'return name of p & "||" & unix id of p\n'
            'end tell'
        )
        r = _osascript(script, timeout=10)
        if r.returncode != 0:
            return {"success": False, "error": r.stderr.strip() or "AppleScript failed"}

        line = r.stdout.strip()
        if "||" in line:
            name, pid_str = line.rsplit("||", 1)
            pid = int(pid_str.strip())
            name = name.strip()
            logger.info(f"Frontmost app: {name} (PID {pid})")
            return {"success": True, "app": {"name": name, "pid": pid}}
        return {"success": False, "error": f"Unable to parse: {line}"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def open_app(app_name: str) -> dict:
    """Open (launch) a specific application on macOS. Supports Chinese names.

    Use this tool when the user asks to open / launch / start an application,
    e.g. "打开微信", "open Safari", "帮我启动 VS Code".

    This tool tries three approaches in order:
    1. Open by display name: e.g. "微信", "Safari"
    2. Open by bundle identifier: e.g. "com.apple.Safari"
    3. Search the filesystem for a matching .app and open by full path

    Args:
        app_name: the application to open. Acceptable forms:
            - display name: "Safari", "微信", "Visual Studio Code"
            - bundle identifier: "com.apple.Safari"
            - absolute path: "/Applications/Safari.app"

    Note:
        If you are unsure the app is installed or what its exact name is,
        call list_installed_apps first to find the correct name.
        If this fails, verify the app name with list_installed_apps and retry.

    Returns:
        {"success": True, "method": "by-name|by-bundle-id|by-path", "app": ...}
    """
    if (e := _darwin()): return e
    app_name = app_name.strip().strip('"').strip("'")
    if not app_name:
        return {"success": False, "error": "app_name must not be empty"}

    # 1) Open by display name / file name
    r = _run(["open", "-a", app_name])
    if r.returncode == 0:
        logger.info(f"Opened app by name: {app_name}")
        return {"success": True, "method": "by-name", "app": app_name}

    # 2) Open by bundle id
    r = _run(["open", "-b", app_name])
    if r.returncode == 0:
        logger.info(f"Opened app by bundle id: {app_name}")
        return {"success": True, "method": "by-bundle-id", "app": app_name}

    # 3) Search filesystem
    path = _find_app(app_name)
    if path:
        r = _run(["open", path])
        if r.returncode == 0:
            logger.info(f"Opened app by path: {path}")
            return {"success": True, "method": "by-path", "app": app_name, "path": path}

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
def activate_app(app_name: str) -> dict:
    """Bring a running application to the foreground (focus it).

    Use this tool when:
    - The user wants to switch to an already-running app ("切换到微信").
    - You need to bring a specific window to the front.

    This is different from open_app: activate_app focuses an already-running
    app, while open_app launches a new instance (or focuses if already running).

    Args:
        app_name: the application name as it appears in the Dock or
                  list_running_apps, e.g. "Safari", "微信", "Terminal".
                  Must match the running app's exact name.

    Returns:
        {"success": True, "app": "Safari"}
    """
    if (e := _darwin()): return e
    app_name = app_name.strip().strip('"').strip("'")
    if not app_name:
        return {"success": False, "error": "app_name must not be empty"}

    try:
        r = _osascript(f'tell application "{_ase(app_name)}" to activate')
        if r.returncode == 0:
            logger.info(f"Activated app: {app_name}")
            return {"success": True, "app": app_name}
        return {"success": False, "error": r.stderr.strip() or "activate failed"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def quit_app(app_name: str) -> dict:
    """Quit (gracefully close) an application (same as Cmd+Q).

    Use this tool when:
    - The user asks to quit/close an app ("退出微信", "quit Safari").
    - You need to close an app to free system resources.

    This sends a normal quit request. Unsaved work may trigger a save dialog.
    If the app is not running, this still returns success (idempotent).
    To force-quit a frozen app, use kill_process instead.

    Args:
        app_name: the application to quit, as shown in list_running_apps,
                  e.g. "Safari", "微信", "Terminal".

    Returns:
        {"success": True, "app": "Safari", "was_running": True/False}
    """
    if (e := _darwin()): return e
    app_name = app_name.strip().strip('"').strip("'")
    if not app_name:
        return {"success": False, "error": "app_name must not be empty"}

    try:
        r = _osascript(f'tell application "{_ase(app_name)}" to quit')
        if r.returncode == 0:
            logger.info(f"Quit app: {app_name}")
            return {"success": True, "app": app_name, "was_running": True}
        # AppleScript error = app not running — this is OK
        if "not running" in (r.stderr or "") or r.returncode == 1:
            logger.info(f"App not running: {app_name}")
            return {"success": True, "app": app_name, "was_running": False}
        return {"success": False, "error": r.stderr.strip() or "quit failed"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def hide_app(app_name: str) -> dict:
    """Hide all windows of an application (Cmd+H equivalent).

    Use this tool when:
    - The user wants to hide an app without quitting it ("隐藏微信").
    - You want to clean up the screen without closing apps.

    The app keeps running in the background; use activate_app to bring it
    back. This uses System Events which works on all apps, even those
    that don't support standard AppleScript.

    Args:
        app_name: the application to hide, as shown in list_running_apps.

    Returns:
        {"success": True, "app": "Safari"}
    """
    if (e := _darwin()): return e
    app_name = app_name.strip().strip('"').strip("'")
    if not app_name:
        return {"success": False, "error": "app_name must not be empty"}

    try:
        r = _osascript(
            f'tell application "System Events" to set visible of process'
            f' "{_ase(app_name)}" to false',
        )
        if r.returncode == 0:
            logger.info(f"Hid app: {app_name}")
            return {"success": True, "app": app_name}
        return {"success": False, "error": r.stderr.strip() or "hide failed"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def open_website(url: str) -> dict:
    """Open a website in the default browser on macOS.

    Use this tool when the user asks to open / visit / go to a website or
    webpage, e.g. "打开百度", "open GitHub", "帮我打开 B 站".

    Args:
        url: the website address, e.g. "www.baidu.com" or "https://github.com".
             - If the user gives a site name in natural language (e.g. 百度, B站),
               convert it to its domain first: "www.baidu.com", "www.bilibili.com".
             - A scheme (http:// or https://) is added automatically if missing.

    Returns:
        {"success": True, "opened": "https://www.baidu.com"}
    """
    if (e := _darwin()): return e
    url = url.strip().strip('"').strip("'")
    if not url:
        return {"success": False, "error": "url must not be empty"}

    # Reject obviously invalid input (injection / spaces)
    if " " in url or "\n" in url or "\t" in url:
        return {"success": False, "error": f"invalid url: {url}"}

    if not url.startswith(("http://", "https://")):
        url = "https://" + url

    r = _run(["open", url])
    if r.returncode == 0:
        logger.info(f"Opened website: {url}")
        return {"success": True, "opened": url}
    return {"success": False, "error": r.stderr.strip() or f"open failed (code {r.returncode})"}


# ═══════════════════════════════════════════════════════════════════════════════
# 5. Process Management / 进程管理
# ═══════════════════════════════════════════════════════════════════════════════

@mcp.tool()
def list_processes(name_filter: str = "", limit: int = 20) -> dict:
    """Search running processes on this Mac by name.

    Returns PID, CPU%, memory%, and full command line for matching processes.

    Use this tool when:
    - The user asks "what processes are running" / "查找某个进程".
    - You need to find the PID of a background process.
    - Before calling kill_process, to verify the correct target.
    - The user asks about CPU/memory usage of specific processes.

    Args:
        name_filter: filter processes whose command line contains this text
                     (case-insensitive). Empty = return top processes by CPU%.
        limit: max number of results (default 20, max 50).

    Returns:
        {"success": True, "count": N,
         "processes": [{"pid": 123, "cpu_pct": 5.2, "mem_pct": 1.3, "command": "..."}]}
    """
    if (e := _darwin()): return e
    limit = min(max(1, limit), 50)
    try:
        if name_filter:
            r = _run(["pgrep", "-lf", name_filter], timeout=10)
            lines = [l for l in r.stdout.strip().splitlines() if l]
            processes = []
            for line in lines[:limit]:
                parts = line.split(None, 1)
                if len(parts) == 2:
                    processes.append({
                        "pid": int(parts[0]),
                        "command": parts[1],
                        "cpu_pct": None,
                        "mem_pct": None,
                    })
        else:
            r = _run(["ps", "aux", "-c", "-r"], timeout=10)
            lines = r.stdout.strip().splitlines()[1:]  # skip header
            processes = []
            for line in lines[:limit]:
                parts = line.split(None, 10)
                if len(parts) >= 11:
                    try:
                        processes.append({
                            "pid": int(parts[1]),
                            "cpu_pct": float(parts[2]),
                            "mem_pct": float(parts[3]),
                            "command": parts[10],
                        })
                    except (ValueError, IndexError):
                        continue

        logger.info(f"Listed {len(processes)} processes (filter={name_filter or 'top'})")
        return {"success": True, "count": len(processes), "processes": processes}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def kill_process(pid_or_name: str, force: bool = False) -> dict:
    """Kill (terminate) a process by PID or by exact name.

    WARNING: Killing system processes can crash your Mac or cause data loss.
    Only kill processes you are CERTAIN are safe to terminate. When in doubt,
    ask the user first before calling this tool.

    Use this tool when:
    - An application is frozen / not responding ("某个应用卡死了").
    - The user explicitly asks to kill a specific process.

    Args:
        pid_or_name: process ID (e.g. "1234") or exact process name (e.g. "Safari").
                     If it looks like a number, it is treated as PID;
                     otherwise as a process name (matched exactly).
        force: if True, sends SIGKILL (-9) — no chance to save data.
               if False (default), sends SIGTERM — graceful shutdown.

    Returns:
        {"success": True, "signal": "SIGTERM", "target": "1234"}
    """
    if (e := _darwin()): return e
    pid_or_name = pid_or_name.strip()
    if not pid_or_name:
        return {"success": False, "error": "pid_or_name must not be empty"}

    signal_name = "SIGKILL" if force else "SIGTERM"
    try:
        if pid_or_name.isdigit():
            flag = "-9" if force else "-15"
            r = _run(["kill", flag, pid_or_name])
        else:
            flag = "-9" if force else ""
            cmd = ["pkill"]
            if flag:
                cmd.append(flag)
            cmd.extend(["-f", pid_or_name])
            r = _run(cmd)

        if r.returncode == 0:
            logger.warning(f"Killed process: {pid_or_name} ({signal_name})")
            return {"success": True, "signal": signal_name, "target": pid_or_name}
        return {
            "success": False,
            "error": r.stderr.strip() or f"kill returned code {r.returncode}",
            "hint": "Process may not exist, or may require force=True.",
        }
    except Exception as ex:
        return {"success": False, "error": str(ex)}


# ═══════════════════════════════════════════════════════════════════════════════
# 6. Audio Control / 音量控制
# ═══════════════════════════════════════════════════════════════════════════════

@mcp.tool()
def get_volume() -> dict:
    """Get the current system output volume level, mute state, and alert volume.

    Use this tool when:
    - The user asks about volume level ("音量是多少").
    - You need to check mute status before/after changing volume.
    - You want to restore volume after a temporary change.

    Returns:
        {"success": True, "output_volume": 50, "muted": False, "alert_volume": 100}
    """
    if (e := _darwin()): return e
    try:
        r = _osascript("get volume settings")
        if r.returncode != 0:
            return {"success": False, "error": r.stderr.strip() or "AppleScript failed"}

        output = r.stdout.strip()
        result: dict = {}
        for m in re.finditer(r"(\w+(?: \w+)?):(\d+)", output):
            key = m.group(1).replace(" ", "_")
            result[key] = int(m.group(2))
        muted_match = re.search(r"output muted:(\w+)", output)
        if muted_match:
            result["muted"] = muted_match.group(1) == "true"

        logger.info(f"Volume: {result.get('output_volume')}, muted={result.get('muted')}")
        return {"success": True, **result}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def set_volume(level: int) -> dict:
    """Set the system output volume to a specific level (0–100).

    Use this tool when:
    - The user asks to change volume ("音量调到50" / "set volume to 80").
    - You want to adjust volume before playing a notification sound.

    This does NOT affect mute state — if the system is muted, unmute first
    with toggle_mute.

    Args:
        level: volume level from 0 (silent) to 100 (maximum).

    Returns:
        {"success": True, "volume": 50}
    """
    if (e := _darwin()): return e
    level = max(0, min(100, int(level)))
    try:
        r = _osascript(f"set volume output volume {level}")
        if r.returncode == 0:
            logger.info(f"Set volume to {level}")
            return {"success": True, "volume": level}
        return {"success": False, "error": r.stderr.strip() or "set volume failed"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def toggle_mute() -> dict:
    """Toggle the system mute state (mute→unmute, unmute→mute).

    Use this tool when:
    - The user asks to mute/unmute ("静音" / "取消静音").
    - You need to quickly silence the Mac.

    This reads the current mute state first, then flips it.

    Returns:
        {"success": True, "muted": True}   — now muted
        {"success": True, "muted": False}  — now unmuted
    """
    if (e := _darwin()): return e
    try:
        r = _osascript("get volume settings")
        muted_match = re.search(r"output muted:(\w+)", r.stdout)
        is_muted = muted_match.group(1) == "true" if muted_match else False

        new_state = "true" if not is_muted else "false"
        r2 = _osascript(f"set volume output muted {new_state}")
        if r2.returncode == 0:
            logger.info(f"Mute toggled to: {not is_muted}")
            return {"success": True, "muted": not is_muted}
        return {"success": False, "error": r2.stderr.strip() or "toggle mute failed"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


# ═══════════════════════════════════════════════════════════════════════════════
# 7. Power & Display / 电源与显示
# ═══════════════════════════════════════════════════════════════════════════════

@mcp.tool()
def lock_screen() -> dict:
    """Lock the Mac screen (turn off display and require password to resume).

    Use this tool when:
    - The user asks to lock their Mac ("锁屏" / "lock my screen").
    - The user is stepping away and wants privacy.
    - You finish a sensitive operation and want to secure the Mac.

    Note:
        This uses 'pmset displaysleepnow' which turns off the display.
        It requires that "Require password immediately after sleep or screen
        saver begins" is enabled in System Settings > Lock Screen for the
        password prompt to appear. (This is the macOS default.)

        No special permissions are needed — this always works.

    Returns:
        {"success": True}
    """
    if (e := _darwin()): return e
    try:
        r = _run(["pmset", "displaysleepnow"], timeout=10)
        if r.returncode == 0:
            logger.info("Screen locked / display slept")
            return {"success": True}
        return {"success": False, "error": r.stderr.strip() or "display sleep failed"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def sleep_display() -> dict:
    """Turn off the display immediately without sleeping the whole Mac.

    Use this tool when:
    - The user wants to turn off the screen ("关屏幕").
    - You want to save power without sleeping the system.
    - Background tasks (downloads, builds) should keep running.

    The Mac stays awake; only the display turns off. Move the mouse or
    press any key to wake the display.

    Returns:
        {"success": True}
    """
    return lock_screen()  # Same mechanism: pmset displaysleepnow


@mcp.tool()
def sleep_system() -> dict:
    """Put the entire Mac to sleep immediately.

    Use this tool when:
    - The user asks to put the Mac to sleep ("让电脑休眠").
    - The user is leaving and wants to save power.

    WARNING: This suspends ALL activity — downloads, builds, network
    connections will all be interrupted. Always confirm with the user
    before calling this tool unless they explicitly asked for sleep.

    Returns:
        {"success": True}
    """
    if (e := _darwin()): return e
    try:
        r = _run(["pmset", "sleepnow"], timeout=10)
        if r.returncode == 0:
            logger.info("System sleeping")
            return {"success": True}
        return {"success": False, "error": r.stderr.strip() or "sleep failed"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


# ═══════════════════════════════════════════════════════════════════════════════
# 8. Screenshots / 截图
# ═══════════════════════════════════════════════════════════════════════════════

@mcp.tool()
def take_screenshot(
    save_path: str = "",
    to_clipboard: bool = False,
    interactive: bool = False,
    include_cursor: bool = False,
) -> dict:
    """Take a screenshot on macOS using the built-in screencapture command.

    Use this tool when:
    - The user asks to take a screenshot ("截图" / "take a screenshot").
    - You want to capture the current screen state.
    - The user needs to share what's on their screen.

    IMPORTANT: The terminal app running mcp_pipe.py needs Screen Recording
    permission in System Settings > Privacy & Security > Screen Recording.
    Without this, screenshots may capture only the wallpaper.

    Args:
        save_path: where to save the screenshot. Default = Desktop with
                   timestamp, e.g. "~/Desktop/screenshot_20260802_223000.png".
        to_clipboard: if True, copies to clipboard instead of saving to file.
                      Ignored if interactive=True (user chooses in the UI).
        interactive: if True, opens the interactive capture tool where the
                     user can select a region/window (press Space for window
                     mode, Escape to cancel). Timeout is 120s for human input.
        include_cursor: if True, includes the mouse cursor in the screenshot.

    Returns:
        {"success": True, "path": "/path/to/file.png", "size_bytes": 123456}
        {"success": True, "copied_to_clipboard": True}
        {"success": True, "cancelled": True}  — user pressed Escape in interactive mode
    """
    if (e := _darwin()): return e
    try:
        cmd = ["screencapture", "-x"]  # -x = no capture sound

        if interactive:
            cmd.append("-i")
            timeout = 120  # human is interacting
        else:
            timeout = 15

        if include_cursor:
            cmd.append("-C")

        if to_clipboard and not interactive:
            cmd.append("-c")
            r = _run(cmd, timeout=timeout)
            if r.returncode == 0:
                logger.info("Screenshot copied to clipboard")
                return {"success": True, "copied_to_clipboard": True}
        else:
            if not save_path:
                ts = time.strftime("%Y%m%d_%H%M%S")
                save_path = os.path.expanduser(f"~/Desktop/screenshot_{ts}.png")
            else:
                save_path = os.path.expanduser(save_path)

            cmd.append(save_path)
            r = _run(cmd, timeout=timeout)

            if r.returncode != 0:
                err = r.stderr.strip()
                if interactive and not err:
                    return {"success": True, "cancelled": True}
                return {"success": False, "error": err or f"screencapture exited with code {r.returncode}"}

            size = os.path.getsize(save_path) if os.path.exists(save_path) else 0
            logger.info(f"Screenshot saved: {save_path} ({size} bytes)")
            return {"success": True, "path": save_path, "size_bytes": size}

    except subprocess.TimeoutExpired:
        return {"success": False, "error": "截图超时 / screenshot timed out"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}



# ═══════════════════════════════════════════════════════════════════════════════
# 9. Finder & Desktop
# ═══════════════════════════════════════════════════════════════════════════════

@mcp.tool()
def reveal_in_finder(path: str) -> dict:
    """Reveal a file or directory in the Finder (opens a Finder window showing it).

    Use this tool when:
    - The user asks to "show in Finder" / "在访达中显示".
    - You saved or generated a file and want to point the user to it.
    - You need to show the user a specific location on disk.

    Args:
        path: the file or directory, e.g. "~/Downloads/report.pdf".

    Returns:
        {"success": True, "path": "/Users/..."}
    """
    if (e := _darwin()): return e
    path = os.path.expanduser(path.strip().strip('"').strip("'"))
    if not path:
        return {"success": False, "error": "path must not be empty"}
    if not os.path.exists(path):
        return {"success": False, "error": f"path does not exist: {path}"}

    try:
        r = _run(["open", "-R", path])
        if r.returncode == 0:
            logger.info(f"Revealed in Finder: {path}")
            return {"success": True, "path": path}
        return {"success": False, "error": r.stderr.strip() or f"open -R failed (code {r.returncode})"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def empty_trash() -> dict:
    """Empty the macOS Trash — permanently deletes all trashed files.

    Use this tool when:
    - The user asks to empty the trash ("清空废纸篓" / "empty trash").
    - The user wants to free up disk space from previously deleted files.

    WARNING: This PERMANENTLY deletes everything currently in the Trash.
    Files cannot be recovered. ALWAYS confirm with the user before calling
    this tool unless they explicitly asked for it.

    Returns:
        {"success": True}
    """
    if (e := _darwin()): return e
    try:
        r = _osascript('tell application "Finder" to empty trash', timeout=30)
        if r.returncode == 0:
            logger.warning("Trash emptied")
            return {"success": True}
        return {"success": False, "error": r.stderr.strip() or "empty trash failed"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def set_wallpaper(image_path: str) -> dict:
    """Set the macOS desktop wallpaper to the given image.

    Use this tool when:
    - The user asks to change their wallpaper ("换壁纸").
    - You downloaded or generated an image for the background.

    Supports: PNG, JPEG, HEIC, TIFF, BMP.

    Args:
        image_path: absolute path to the image, e.g. "/Users/xxx/Pictures/wall.jpg".

    Returns:
        {"success": True, "wallpaper": "/path/to/image.jpg"}
    """
    if (e := _darwin()): return e
    image_path = os.path.expanduser(image_path.strip().strip('"').strip("'"))
    if not image_path:
        return {"success": False, "error": "image_path must not be empty"}
    if not os.path.isfile(image_path):
        return {"success": False, "error": f"image file not found: {image_path}"}

    abs_path = os.path.abspath(image_path)
    try:
        script = (
            f'tell application "System Events" to tell every desktop'
            f' to set picture to POSIX file "{_ase(abs_path)}"'
        )
        r = _osascript(script, timeout=15)
        if r.returncode == 0:
            logger.info(f"Wallpaper set to: {abs_path}")
            return {"success": True, "wallpaper": abs_path}
        return {"success": False, "error": r.stderr.strip() or "set wallpaper failed"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


# ═══════════════════════════════════════════════════════════════════════════════
# 10. Network
# ═══════════════════════════════════════════════════════════════════════════════

@mcp.tool()
def get_network_info() -> dict:
    """Get network information: local IP, WiFi SSID, MAC address, DNS servers.

    Use this tool when:
    - The user asks about network status ("网络信息" / "what's my IP").
    - You need to know the current WiFi network or local IP.
    - Troubleshooting network connectivity.

    Returns:
        {"success": True, "hostname": "...", "local_ip": "192.168.1.5",
         "wifi_ssid": "MyWiFi", "mac_address_en0": "aa:bb:cc:...",
         "dns_servers": ["8.8.8.8", ...]}
    """
    if (e := _darwin()): return e
    result: dict = {}
    try:
        result["hostname"] = socket.gethostname()
    except Exception:
        result["hostname"] = None

    # Local IP
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.settimeout(3)
        s.connect(("8.8.8.8", 80))
        result["local_ip"] = s.getsockname()[0]
        s.close()
    except Exception:
        result["local_ip"] = None

    # WiFi SSID (dynamically discover interface)
    try:
        r = _run(["networksetup", "-listallhardwareports"], timeout=10)
        wifi_dev = None
        lines = r.stdout.splitlines()
        for i, line in enumerate(lines):
            if "Wi-Fi" in line or "AirPort" in line:
                if i + 1 < len(lines):
                    m = re.search(r"Device:\s*(\S+)", lines[i + 1])
                    if m:
                        wifi_dev = m.group(1)
                        break
        if wifi_dev:
            r2 = _run(["networksetup", "-getairportnetwork", wifi_dev], timeout=10)
            if r2.returncode == 0:
                m = re.search(r"Current Wi-Fi Network:\s*(.+)", r2.stdout)
                if m:
                    result["wifi_ssid"] = m.group(1).strip()
            if "wifi_ssid" not in result:
                result["wifi_ssid"] = None
        else:
            result["wifi_ssid"] = None
    except Exception:
        result["wifi_ssid"] = None

    # MAC address
    try:
        r = _run(["ifconfig", "en0"], timeout=10)
        m = re.search(r"ether\s+(\S+)", r.stdout)
        if m:
            result["mac_address_en0"] = m.group(1)
    except Exception:
        result["mac_address_en0"] = None

    # DNS
    try:
        r = _run(["scutil", "--dns"], timeout=10)
        dns_servers: list[str] = []
        for m in re.finditer(r"nameserver\[(\d+)\]\s*:\s*(\S+)", r.stdout):
            ns = m.group(2)
            if ns not in dns_servers:
                dns_servers.append(ns)
        result["dns_servers"] = dns_servers if dns_servers else None
    except Exception:
        result["dns_servers"] = None

    logger.info(f"Network: IP={result.get('local_ip')}, WiFi={result.get('wifi_ssid')}")
    return {"success": True, **result}

# 11. Search / Spotlight search
# 12. Shell Execution
# Entry point

VALID_KINDS = {
    "app": "com.apple.application", "application": "com.apple.application",
    "document": "public.item", "folder": "public.folder",
    "image": "public.image", "movie": "public.movie",
    "music": "public.audio", "audio": "public.audio",
    "pdf": "com.adobe.pdf", "archive": "public.archive",
    "code": "public.source-code", "text": "public.text",
    "spreadsheet": "public.spreadsheet", "presentation": "public.presentation",
}


@mcp.tool()
def spotlight_search(query: str, kind: str = "", max_results: int = 30) -> dict:
    """Search files and apps using macOS Spotlight (mdfind), extremely fast.

    Spotlight indexes the entire filesystem. Much faster than find/ls -R.

    Use this tool when:
    - User asks "where is file X" / "find files about..."
    - You need to locate PDFs, images, documents matching a topic.
    - open_app failed and you need to find the .app path on disk.

    Args:
        query: search query (filename, content keyword, or app name).
               Examples: "report", "contract", "vacation photo"
        kind: file type filter. Options: "app", "application", "document",
              "folder", "image", "movie", "music", "audio", "pdf",
              "archive", "code", "text", "spreadsheet", "presentation".
              Leave empty to search all.
        max_results: max results (default 30, max 200).

    Returns:
        {"success": True, "count": N,
         "results": [{"name": "f.pdf", "path": "/Users/.../f.pdf"}, ...]}
    """
    if (e := _darwin()): return e
    max_results = min(max(1, max_results), 200)
    query = query.strip().strip('"').strip("'")
    if not query:
        return {"success": False, "error": "query must not be empty"}

    try:
        expr_parts = []
        if kind:
            kl = kind.lower()
            if kl not in VALID_KINDS:
                return {
                    "success": False,
                    "error": f"Unknown kind: '{kind}'. Allowed: {', '.join(sorted(VALID_KINDS.keys()))}",
                }
            expr_parts.append(f"kMDItemContentTypeTree == '{VALID_KINDS[kl]}'")

        expr = f"kMDItemDisplayName == '*{query}*'cd"
        if expr_parts:
            expr = f"({' && '.join(expr_parts)}) && " + expr

        r = _run(["mdfind", expr], timeout=15)
        lines = [l for l in r.stdout.strip().splitlines() if l]

        results = [{"name": os.path.basename(p), "path": p} for p in lines[:max_results]]
        logger.info(f"Spotlight '{query}' (kind={kind or 'all'}): {len(results)} results")
        return {"success": True, "count": len(results), "results": results}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


@mcp.tool()
def run_shell_command(command: str, working_dir: str = "", timeout: int = 30) -> dict:
    """Execute an arbitrary shell command on this Mac.

    This is the most powerful tool. Use ONLY as fallback when no dedicated
    tool exists for the task.

    SAFETY RULES:
    1. CONFIRM DESTRUCTIVE COMMANDS: Before rm, rm -rf, sudo, shutdown,
       reboot, diskutil, killall, or any command that deletes/modifies files
       or system settings, MUST ask user for confirmation.
    2. PREFER DEDICATED TOOLS: Use specialized tools (get_system_info,
       open_app, set_volume, spotlight_search, etc.) when available.
       This is the LAST RESORT.
    3. REPORT RESULT: Tell user what was executed and the outcome.
    4. SAFE READ-ONLY: ls, cat, grep, find, du, df, ps, top, which,
       echo, pwd, head, tail, wc are always safe without confirmation.

    Args:
        command: shell command with pipes/redirects/chaining.
                 Examples: "ls ~/Downloads", "brew list", "cat notes.txt"
        working_dir: run directory (default = home).
        timeout: max seconds (default 30, max 120).

    Returns:
        {"success": bool, "exit_code": int, "stdout": str, "stderr": str}
    """
    if (e := _darwin()): return e
    timeout = min(max(5, timeout), 120)
    cwd = os.path.expanduser(working_dir) if working_dir else None
    if cwd and not os.path.isdir(cwd):
        return {"success": False, "error": f"working_dir does not exist: {cwd}"}

    command = command.strip().strip('"').strip("'")
    if not command:
        return {"success": False, "error": "command must not be empty"}

    try:
        r = subprocess.run(
            command, shell=True, cwd=cwd,
            capture_output=True, text=True, encoding="utf-8",
            errors="replace", timeout=timeout,
        )
        stdout = r.stdout[-8000:] if len(r.stdout) > 8000 else r.stdout
        stderr = r.stderr[-8000:] if len(r.stderr) > 8000 else r.stderr
        truncated = len(r.stdout) > 8000 or len(r.stderr) > 8000

        short = command[:120] + ("..." if len(command) > 120 else "")
        logger.info(f"Ran: {short} (exit={r.returncode})")

        result: dict = {"success": r.returncode == 0, "exit_code": r.returncode,
                         "stdout": stdout, "stderr": stderr}
        if truncated:
            result["_truncated"] = True
        return result
    except subprocess.TimeoutExpired:
        return {"success": False, "error": f"Command timed out ({timeout}s)"}
    except Exception as ex:
        return {"success": False, "error": str(ex)}


if __name__ == "__main__":
    mcp.run(transport="stdio")
