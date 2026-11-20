#!/usr/bin/env python3
"""Run the native #28 smoke scenario. Build requirements: Go, Git, macOS GUI."""
import argparse
import base64
import fcntl
import hashlib
import http.server
import json
import os
from pathlib import Path
import plistlib
import socket
import sqlite3
import subprocess
import threading
import time

DESKTOP = Path(__file__).resolve().parent
REPO = DESKTOP.parents[1]
REMOTE = "https://github.com/example/context.git"
SESSION = "ctxferry-smoke-only-session"


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs).strip()


def wait_for(check, timeout=20):
    until = time.monotonic() + timeout
    while time.monotonic() < until:
        try:
            if result := check():
                return result
        except (ConnectionError, FileNotFoundError, OSError):
            pass
        time.sleep(0.1)
    raise AssertionError("Timed out waiting for " + check.__name__)


class Metadata(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/repos/example/context":
            self.send_error(404)
            return
        if self.headers.get("Authorization") != "Bearer smoke-only-git":
            self.send_error(401)
            return
        self.send_response(self.server.reply)
        self.end_headers()
        self.wfile.write(b'{"id":42,"full_name":"example/context","private":true}')

    def log_message(self, *args):
        pass


class Window:
    def __init__(self, endpoint):
        self.endpoint = str(endpoint)

    def call(self, method, **params):
        with socket.socket(socket.AF_UNIX) as conn:
            conn.settimeout(20)
            conn.connect(self.endpoint)
            request = json.dumps({"id": 1, "method": method, "params": params})
            conn.sendall(request.encode() + b"\n")
            line = conn.makefile("rb").readline()
            if not line:
                raise ConnectionError("The desktop closed its automation connection")
            reply = json.loads(line)
        if "error" in reply:
            if reply["error"]["code"] == "no_window":
                raise ConnectionError("Native window is not ready")
            raise AssertionError(reply["error"])
        return reply["result"]

    def nodes(self):
        return self.call("tree")["nodes"]

    def label(self, text):
        def exact_label():
            return next((node for node in self.nodes() if node.get("label") == text), None)

        return wait_for(exact_label)

    def click(self, label):
        node = self.label(label)
        bounds = node["bounds"]
        self.call("input", events=[{"type": "click",
                                    "x": bounds["x"] + bounds["width"] / 2,
                                    "y": bounds["y"] + bounds["height"] / 2}])

    def capture(self, directory, name, images):
        nodes = self.nodes()
        assert SESSION not in json.dumps(nodes)
        assert all(node["role"] != "webView" for node in nodes)
        window = self.call("windows")["windows"][0]
        assert window["kind"] == "content" and window["visible"], window
        frame = self.call("capture")
        png = base64.b64decode(frame["png"])
        assert png.startswith(b"\x89PNG\r\n\x1a\n")
        path = directory / (name + ".png")
        path.write_bytes(png)
        images[name] = {"sha256": hashlib.sha256(png).hexdigest(),
                        "width": frame["width"], "height": frame["height"]}


def smoke(root, server):
    cli, binary = root / "ctxferry", root / "desktop"
    run("go", "build", "-tags", "ctxferry_test", "-o", cli, "./cmd/ctxferry", cwd=REPO)
    run("go", "build", "-tags", "desktop_smoke,ctxferry_test", "-o", binary, ".", cwd=DESKTOP)
    origin = root / "remote.git"
    run("git", "init", "--bare", "--quiet", origin)
    home = root / "home"
    home.mkdir()
    env = {k: os.environ[k] for k in ("PATH", "TMPDIR") if k in os.environ}
    env.update(HOME=str(home), GH_TOKEN="smoke-only-git",
               CTXFERRY_TEST_API=f"http://127.0.0.1:{server.server_port}",
               CTXFERRY_TEST_REMOTE=str(origin))
    checkout = {side: root / ("checkout-" + side) for side in ("a", "b")}
    for directory in checkout.values():
        directory.mkdir()
    data_dir = {side: root / ("data-" + side) for side in checkout}

    def cli_call(side, *args, error=None, extra=None):
        proc = subprocess.run([cli, "--data-dir", data_dir[side], "--format=json", *args],
                              cwd=checkout[side], env={**env, **(extra or {})}, capture_output=True)
        wire = json.loads(proc.stdout or proc.stderr)
        if error:
            assert not wire["ok"] and wire["error"]["code"] == error, wire
        else:
            assert wire["ok"] and proc.returncode == 0, wire
        return wire.get("data")

    def doc(side, title, body, prior=None):
        path = root / "body.md"
        path.write_text(body)
        update = [prior["id"], "--if-revision", prior["revision"]] if prior else []
        return cli_call(side, "context", "put", *update, "--title", title, "--purpose", "notes",
                        "--source", "desktop-smoke", "--storage", "shared", "--disclosure", "full",
                        "--body-file", str(path))["document"]

    cli_call("a", "init")
    cli_call("a", "project", "bind", ".", "--slug", "demo")
    d = doc("a", "测试环境说明", "共同基线")
    cli_call("a", "sync", "enable", "--remote", REMOTE)
    preview = cli_call("a", "sync", "preview")["previewId"]
    cli_call("a", "sync", "init", "--preview", preview)
    cli_call("b", "join", REMOTE, "--all-projects")
    cli_call("b", "project", "bind", ".", "--project", "demo")
    desktop_checkout = checkout["b"] / "tools"
    desktop_checkout.mkdir()
    cli_call("b", "project", "bind", str(desktop_checkout), "--slug", "tools")
    doc("a", "测试环境说明", "远端修订", d)
    cli_call("a", "sync")
    doc("b", "测试环境说明", "本机修订", d)
    cli_call("b", "sync")
    pending = doc("b", "运行手册", "待发布修订")
    doc("b", "部署说明", "第二条待发布修订")
    fixture = cli_call("b", "sync", "status")
    assert len(fixture["conflicts"]) == 1 and fixture["pending"] in (2, 3), fixture
    assert fixture["head"] and fixture["history"]["rawBytes"] > 0

    # A synthetic SecretBinding supplies profile metadata to the real catalog.
    # The desktop exercises the environment presence probe and never uses this value.
    db_path = data_dir["b"] / "workspaces/default/catalog.sqlite"
    with sqlite3.connect(db_path) as db:
        project = db.execute("SELECT id FROM projects WHERE slug='demo'").fetchone()[0]
        workspace = db.execute("SELECT id FROM workspace").fetchone()[0]
        device = db.execute("SELECT id FROM device").fetchone()[0]
        secret = {"schemaVersion": 1, "id": "5357c930-0e08-4aa8-a160-365b1714b6fe",
                  "kind": "secret-binding", "workspaceId": workspace, "scope": {"kind": "project", "id": project},
                  "revision": "a8beb82d-ce20-4ef4-aa95-2c0aeb7eb4b2", "previousRevision": "",
                  "operationId": "5ff582e3-97be-40d4-ae66-d28239ad2fa4", "deleted": False,
                  "provenance": {"deviceId": device, "updatedAt": d["updatedAt"]},
                  "policy": {"storage": "shared", "agentDisclosure": "reference"},
                  "payload": {"alias": "smoke-reader", "resourceId": "74c8ca9e-f369-4b9a-872e-5c48bcd35e97",
                              "purpose": "test", "type": "token",
                              "providerProfile": {"provider": "infisical", "name": "smoke", "method": "universal-auth"},
                              "locator": {"projectId": "synthetic", "environment": "dev", "path": "/", "key": "SMOKE"}}}
        db.execute("INSERT INTO records VALUES(?,?,?,?)",
                   (secret["id"], project, secret["kind"], json.dumps(secret)))

    label = "com.ctxferry.ctxferry-sync-" + hashlib.sha256(os.fsencode(data_dir["b"].resolve())).hexdigest()[:24]
    job = f"gui/{os.getuid()}/{label}"
    plist = root / (label + ".plist")
    gate = root / "allow-service-fetch"
    plist.write_bytes(plistlib.dumps({"Label": label, "RunAtLoad": True,
                                     "ProgramArguments": [str(cli), "--data-dir", str(data_dir["b"]), "sync", "serve"],
                                     "EnvironmentVariables": {**env, "CTXFERRY_TEST_FETCH_GATE": str(gate)}}))
    service_pid = None

    def job_pid():
        state = run("/bin/launchctl", "print", job)
        return next((int(line.split("=")[1]) for line in state.splitlines()
                     if line.strip().startswith("pid =")), None)

    endpoint = root / "native.sock"
    app_env = {**env, "CTXFERRY_DATA_DIR": str(data_dir["b"]), "MYGO_AUTOMATION": str(endpoint),
               "CTXFERRY_INFISICAL_WORKSPACE_ID": workspace, "CTXFERRY_INFISICAL_DEVICE_ID": device,
               "CTXFERRY_INFISICAL_PROFILE": "smoke", "CTXFERRY_INFISICAL_ACCESS_TOKEN": SESSION}
    window = Window(endpoint)
    images = {}
    lock = Path(run("git", "rev-parse", "--path-format=absolute", "--git-common-dir", cwd=REPO)) / "ctxferry-desktop-gui.lock"
    try:
        with lock.open("a") as lockfile:
            fcntl.flock(lockfile, fcntl.LOCK_EX)
            with (root / "desktop.log").open("w") as diagnostics:
                proc = subprocess.Popen([binary], env=app_env, stdout=diagnostics, stderr=diagnostics)
                try:
                    window.label("设备总览")
                    window.label("smoke · 已配置本机运行会话 · 登录未经验证")
                    window.capture(root, "01-overview-configured", images)
                    window.click("查看冲突")
                    window.label("测试环境说明")
                    window.label("复制比较命令")
                    window.click("全部")
                    window.label("运行手册")
                    window.click("检查远端")
                    window.label("Git 历史体积")
                    wait_for(lambda: any("已核验私有仓库" in n.get("label", "") for n in window.nodes()))
                    window.capture(root, "02-sync-configured", images)

                    window.click("创建备份…")
                    window.label("创建工作区备份")
                    output = root / "verified-backup"
                    field = window.label("备份目标目录")
                    window.call("act", id=field["id"], action="setValue", text=str(output))
                    window.capture(root, "03-backup-confirm", images)
                    window.click("确认创建备份")
                    window.label("备份已创建")
                    wait_for(lambda: any("校验清单 manifest.json" in n.get("label", "") for n in window.nodes()))
                    wait_for(lambda: (output / "manifest.json").is_file())
                    window.capture(root, "04-backup-receipt", images)
                    manifest = json.loads((output / "manifest.json").read_text())
                    assert not manifest.get("includesLocalOnly") and manifest["head"] == fixture["head"], manifest
                    for filename, key in (("catalog.sqlite", "catalogSha256"), ("published.bundle", "bundleSha256")):
                        assert hashlib.sha256((output / filename).read_bytes()).hexdigest() == manifest[key]

                    server.reply = 503
                    cli_call("b", "sync", error="offline", extra={"CTXFERRY_TEST_FETCH_FAILURE": "offline"})
                    window.click("刷新本地视图")
                    window.label("上次同步时远端不可用。下面仍显示本机记录和最后确认的版本。")
                    window.capture(root, "05-sync-offline", images)
                    server.reply = 200

                    # Run a real user service alongside the window. Hold the Git
                    # round before it reads the remote so publication proves the
                    # already-running job continued after File > Close Window.
                    with sqlite3.connect(db_path) as db:
                        db.execute("DELETE FROM records WHERE id=?", (secret["id"],))
                    run("/bin/launchctl", "bootstrap", job.rsplit("/", 1)[0], plist)
                    service_pid = wait_for(job_pid)
                    wait_for(lambda: gate.with_suffix(".entered").is_file())
                    assert cli_call("b", "sync", "status")["head"] == fixture["head"]
                    try:
                        window.call("menu", path=["File", "Close Window"])
                    except ConnectionError:
                        pass

                    def closed_window():
                        if proc.poll() is not None:
                            return True
                        return not window.call("windows")["windows"]

                    wait_for(closed_window)
                finally:
                    if proc.poll() is None:
                        proc.terminate()
                    proc.wait(timeout=10)

        assert service_pid and job_pid() == service_pid
        gate.touch()
        wait_for(lambda: cli_call("b", "sync", "status")["head"] != fixture["head"])
        assert job_pid() == service_pid
        cli_call("a", "sync")
        assert cli_call("a", "context", "get", pending["id"])["document"]["body"] == "待发布修订"
    finally:
        if service_pid or plist.exists():
            subprocess.run(["/bin/launchctl", "bootout", job], capture_output=True)

    receipt = {"revision": run("git", "rev-parse", "HEAD", cwd=REPO),
               "fixture": {"confirmedHead": fixture["head"], "pending": fixture["pending"],
                           "conflicts": len(fixture["conflicts"]), "historyBytes": fixture["history"]["rawBytes"]},
               "backupManifest": manifest, "continuedUserServicePID": service_pid, "images": images}
    (root / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
    print(json.dumps(receipt, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("evidence_directory", type=Path, help="A new isolated directory, under /tmp")
    root = parser.parse_args().evidence_directory.resolve()
    if len(os.fsencode(root / "native.sock")) >= 104:
        parser.error("use a shorter evidence directory for the macOS socket")
    root.mkdir(mode=0o700)
    with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Metadata) as server:
        server.reply = 200
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            smoke(root, server)
        finally:
            server.shutdown()
            thread.join()


if __name__ == "__main__":
    main()
