"""
my-dns Web UI
管理API・log-analyzer・bl-manager を呼び出してブラウザから操作できるWeb UI。
Usage: sudo python3 webui/app.py
"""

import json
import os
import shutil
import subprocess
import sys
from flask import Flask, jsonify, render_template, request

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
BASE_DIR      = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN_DIR       = os.path.join(BASE_DIR, "bin")
CONF_DIR      = "/usr/local/etc/my-dns"
LOG_DIR       = "/var/log/my-dns"
QUERY_LOG     = os.path.join(LOG_DIR, "query.log")
LOCAL_BL      = os.path.join(BASE_DIR, "blocklist.txt")
SYSTEM_BL     = os.path.join(CONF_DIR, "blocklist.txt")
LOG_ANALYZER  = os.path.join(BIN_DIR, "log-analyzer")
BL_MANAGER    = os.path.join(BIN_DIR, "bl-manager")
MGMT_API      = "http://127.0.0.1:8080"
MAKEFILE_DIR  = BASE_DIR

app = Flask(__name__)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def run_cmd(args: list[str], *, text=True, timeout=15) -> tuple[bool, str]:
    """コマンド実行。(ok, output) を返す。"""
    try:
        result = subprocess.run(
            args,
            capture_output=True,
            text=text,
            timeout=timeout,
        )
        out = result.stdout + (result.stderr if result.returncode != 0 else "")
        return result.returncode == 0, out.strip()
    except subprocess.TimeoutExpired:
        return False, "timeout"
    except Exception as e:
        return False, str(e)


def _http_get(url: str, timeout: int = 3) -> dict:
    ok, out = run_cmd(["curl", "-sf", "--max-time", str(timeout), url])
    if not ok:
        raise RuntimeError(out or "curl failed")
    return json.loads(out)


def _http_post(url: str, timeout: int = 5) -> dict:
    ok, out = run_cmd(["curl", "-sf", "--max-time", str(timeout), "-X", "POST", url])
    if not ok:
        raise RuntimeError(out or "curl failed")
    return json.loads(out)


def reload_server() -> tuple[bool, str]:
    """管理APIのリロードエンドポイントを呼び出す。"""
    try:
        data = _http_post(f"{MGMT_API}/reload")
        return True, f"リロード完了: {data.get('entries', '?')} エントリ"
    except Exception as e:
        return False, f"リロードAPI失敗: {e}"


def backup_to_local() -> tuple[bool, str]:
    """システムBL → ローカルプロジェクトにバックアップ（Git管理用）。"""
    try:
        shutil.copy2(SYSTEM_BL, LOCAL_BL)
        return True, f"{SYSTEM_BL} → {LOCAL_BL} にバックアップしました"
    except Exception as e:
        return False, f"バックアップ失敗: {e}"


# ---------------------------------------------------------------------------
# Pages
# ---------------------------------------------------------------------------

@app.route("/")
def index():
    return render_template("index.html")


# ---------------------------------------------------------------------------
# API: Status
# ---------------------------------------------------------------------------

@app.route("/api/status")
def api_status():
    result = {}
    try:
        result["metrics"] = _http_get(f"{MGMT_API}/metrics")
    except Exception as e:
        result["metrics"] = {"error": str(e)}
    try:
        result["health"] = _http_get(f"{MGMT_API}/health")
    except Exception as e:
        result["health"] = {"error": str(e)}
    return jsonify(result)


# ---------------------------------------------------------------------------
# API: Logs
# ---------------------------------------------------------------------------

VALID_LOG_CMDS = {"summary", "top-domains", "top-blocked", "top-clients", "timeline", "errors"}
# JSON出力対応コマンド（-json フラグ使用）
JSON_CAPABLE_CMDS = {"top-domains", "top-blocked", "top-clients", "summary"}

@app.route("/api/logs/<cmd>")
def api_logs(cmd: str):
    if cmd not in VALID_LOG_CMDS:
        return jsonify({"error": f"不明なコマンド: {cmd}"}), 400
    n = request.args.get("n", "20")
    # JSON形式で取得できるコマンドは構造化データを返す
    if cmd in JSON_CAPABLE_CMDS:
        ok, out = run_cmd([LOG_ANALYZER, "-log", QUERY_LOG, "-n", n, "-json", cmd])
        if ok:
            try:
                data = json.loads(out)
                return jsonify({"ok": True, "data": data, "format": "json"})
            except Exception:
                pass
    ok, out = run_cmd([LOG_ANALYZER, "-log", QUERY_LOG, "-n", n, cmd])
    return jsonify({"ok": ok, "output": out, "format": "text"})


@app.route("/api/logs/tail")
def api_logs_tail():
    n = request.args.get("n", "50")
    ok, out = run_cmd([LOG_ANALYZER, "-log", QUERY_LOG, "-json", "tail", str(n)])
    if ok:
        try:
            data = json.loads(out)
            return jsonify({"ok": True, "data": data, "format": "json"})
        except Exception:
            pass
    ok, out = run_cmd([LOG_ANALYZER, "-log", QUERY_LOG, "tail", str(n)])
    return jsonify({"ok": ok, "output": out, "format": "text"})


@app.route("/api/logs/search", methods=["POST"])
def api_logs_search():
    body = request.get_json(force=True)
    pattern = (body.get("pattern") or "").strip()
    if not pattern:
        return jsonify({"error": "pattern が必要です"}), 400
    ok, out = run_cmd([LOG_ANALYZER, "-log", QUERY_LOG, "-json", "search", pattern])
    if ok:
        try:
            data = json.loads(out)
            return jsonify({"ok": True, "data": data, "format": "json"})
        except Exception:
            pass
    ok, out = run_cmd([LOG_ANALYZER, "-log", QUERY_LOG, "search", pattern])
    return jsonify({"ok": ok, "output": out, "format": "text"})


# ---------------------------------------------------------------------------
# API: Blocklist
# ---------------------------------------------------------------------------

@app.route("/api/blocklist")
def api_blocklist_list():
    # システムBL（AUTO_LEARNED含む）を直接読む
    ok, out = run_cmd([BL_MANAGER, "-file", SYSTEM_BL, "list"])
    entries = [l for l in out.splitlines() if l and not l.startswith("(")]
    return jsonify({"ok": ok, "entries": entries})


@app.route("/api/blocklist/add", methods=["POST"])
def api_blocklist_add():
    body = request.get_json(force=True)
    domain = (body.get("domain") or "").strip().lower()
    if not domain:
        return jsonify({"error": "domain が必要です"}), 400
    # システムBLに直接追加 → リロードAPI呼び出し
    ok, out = run_cmd([BL_MANAGER, "-file", SYSTEM_BL, "add", domain])
    if not ok:
        return jsonify({"error": out}), 500
    reload_ok, reload_msg = reload_server()
    return jsonify({"ok": reload_ok, "add_result": out, "sync": reload_msg})


@app.route("/api/blocklist/remove", methods=["POST"])
def api_blocklist_remove():
    body = request.get_json(force=True)
    domain = (body.get("domain") or "").strip().lower()
    if not domain:
        return jsonify({"error": "domain が必要です"}), 400
    # システムBLから直接削除 → リロードAPI呼び出し
    ok, out = run_cmd([BL_MANAGER, "-file", SYSTEM_BL, "remove", domain])
    if not ok:
        return jsonify({"error": out}), 500
    reload_ok, reload_msg = reload_server()
    return jsonify({"ok": reload_ok, "remove_result": out, "sync": reload_msg})


@app.route("/api/blocklist/reload", methods=["POST"])
def api_blocklist_reload():
    ok, msg = reload_server()
    return jsonify({"ok": ok, "message": msg})


@app.route("/api/blocklist/backup", methods=["POST"])
def api_blocklist_backup():
    """システムBL → ローカルプロジェクトにバックアップ（Git管理用）。"""
    ok, msg = backup_to_local()
    return jsonify({"ok": ok, "message": msg})


@app.route("/api/blocklist/stats")
def api_blocklist_stats():
    ok, out = run_cmd([BL_MANAGER, "-file", SYSTEM_BL, "stats"])
    return jsonify({"ok": ok, "output": out})


# ---------------------------------------------------------------------------
# API: DNS Provider Switch
# ---------------------------------------------------------------------------

VALID_PROVIDERS = {"nextdns", "adguard"}

@app.route("/api/switch/<provider>", methods=["POST"])
def api_switch(provider: str):
    if provider not in VALID_PROVIDERS:
        return jsonify({"error": f"不明なプロバイダー: {provider}"}), 400
    target = "switch-nextdns" if provider == "nextdns" else "switch-adguard"
    ok, out = run_cmd(["make", "-C", MAKEFILE_DIR, target], timeout=20)
    return jsonify({"ok": ok, "output": out})


@app.route("/api/cache/flush", methods=["POST"])
def api_cache_flush():
    try:
        return jsonify(_http_post(f"{MGMT_API}/cache/flush"))
    except Exception as e:
        return jsonify({"error": str(e)}), 500


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    if os.geteuid() != 0:
        print("⚠️  Warning: rootで実行していません。ログ読み込みに失敗する場合があります。")
        print("   推奨: sudo python3 webui/app.py")
    print(f"🌐 DNS管理UI: http://0.0.0.0:8888  (LAN: http://192.168.1.203:8888)")
    app.run(host="0.0.0.0", port=8888, debug=False)
