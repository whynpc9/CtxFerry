#!/usr/bin/env python3
"""Capture the production native desktop with isolated synthetic Workspace data."""

import argparse
import base64
import hashlib
import http.server
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import time

DESKTOP = Path(__file__).resolve().parent
REPO = DESKTOP.parents[1]
REMOTE = "https://github.com/example/redesign.git"
SENTINEL = "synthetic-provider-session-not-a-secret"


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs).strip()


def wait_for(check, timeout=25):
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
        if self.path != "/repos/example/redesign" or self.headers.get("Authorization") != "Bearer synthetic-git":
            self.send_error(404)
            return
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'{"id":42,"full_name":"example/redesign","private":true}')

    def log_message(self, *args):
        pass


class Window:
    def __init__(self, endpoint, images, states):
        self.endpoint = str(endpoint)
        self.images = images
        self.states = states

    def call(self, method, **params):
        with socket.socket(socket.AF_UNIX) as conn:
            conn.settimeout(25)
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
        return wait_for(lambda: next((node for node in self.nodes() if node.get("label") == text), None))

    def click(self, text):
        node = self.label(text)
        self.call("act", id=node["id"], action="scrollIntoView")
        node = self.label(text)
        bounds = node["bounds"]
        self.call("input", events=[{"type": "click", "x": bounds["x"] + bounds["width"] / 2,
                                    "y": bounds["y"] + bounds["height"] / 2}])

    def click_prefix(self, prefix):
        node = wait_for(lambda: next((node for node in self.nodes() if node.get("label", "").startswith(prefix)), None))
        self.call("act", id=node["id"], action="scrollIntoView")
        bounds = node["bounds"]
        self.call("input", events=[{"type": "click", "x": bounds["x"] + bounds["width"] / 2,
                                    "y": bounds["y"] + bounds["height"] / 2}])

    def set_value(self, label, text):
        field = self.label(label)
        self.call("act", id=field["id"], action="setValue", text=text)

    def capture(self, root, name, state):
        nodes = self.nodes()
        assert SENTINEL not in json.dumps(nodes)
        assert all(node["role"] != "webView" for node in nodes)
        native = self.call("windows")["windows"][0]
        assert native["kind"] == "content" and native["visible"], native
        frame = self.call("capture")
        png = base64.b64decode(frame["png"])
        assert png.startswith(b"\x89PNG\r\n\x1a\n")
        path = root / (name + ".png")
        path.write_bytes(png)
        self.images[name] = {"sha256": hashlib.sha256(png).hexdigest(),
                             "width": frame["width"], "height": frame["height"], "state": state}


def cli_fixture(root, server_port, binary, home):
    origin = root / "remote.git"
    run("git", "init", "--bare", "--quiet", origin)
    env = {"PATH": os.environ["PATH"], "HOME": str(home), "GH_TOKEN": "synthetic-git",
           "CTXFERRY_TEST_API": f"http://127.0.0.1:{server_port}", "CTXFERRY_TEST_REMOTE": str(origin)}
    if "TMPDIR" in os.environ:
        env["TMPDIR"] = os.environ["TMPDIR"]
    checkout = {side: root / ("checkout-" + side) for side in ("a", "b")}
    data = {side: root / ("data-" + side) for side in ("a", "b")}
    for directory in checkout.values():
        directory.mkdir()
        run("git", "init", "-q", directory)
        run("git", "-C", directory, "-c", "user.email=synthetic@example.invalid", "-c", "user.name=Synthetic",
            "commit", "-q", "--allow-empty", "-m", "Synthetic fixture")

    def call(side, *args):
        proc = subprocess.run([binary, "--data-dir", data[side], "--format=json", *args],
                              cwd=checkout[side], env=env, capture_output=True)
        wire = json.loads(proc.stdout or proc.stderr)
        assert wire["ok"] and proc.returncode == 0, (args, wire)
        return wire.get("data")

    def document(side, title, body, prior=None):
        source = root / (side + "-document.md")
        source.write_text(body)
        update = [prior["id"], "--if-revision", prior["revision"]] if prior else []
        return call(side, "context", "put", *update, "--title", title, "--purpose", "operations",
                    "--source", "synthetic-desktop-review", "--storage", "shared", "--disclosure", "full",
                    "--body-file", str(source))["document"]

    call("a", "init")
    binding = call("a", "project", "bind", ".", "--slug", "atlas-api")["binding"]
    call("a", "profile", "create", "staging")
    call("a", "config", "set", "timeoutSeconds", "60", "--layer", "project")
    call("a", "config", "set", "timeoutSeconds", "20", "--layer", "profile", "--profile", "staging")
    resource = call("a", "resource", "add", "--identity", "postgres-primary", "--kind", "database", "--stage", "dev",
                    "--purpose", "Preview database", "--storage", "shared", "--endpoint", "db.example.com:5432")
    task = call("a", "task", "add", "deploy-preview", "--purpose", "Deploy the preview environment",
                "--membership", binding["membershipId"], "--working-directory", ".", "--program", "deploy",
                "--storage", "shared", "--resource-slot", '{"name":"db","kind":"database","stage":"dev"}',
                "--credential-requirement", '{"name":"db-password","resourceSlot":"db","purpose":"preview-runtime","type":"connection-string","env":"DATABASE_URL"}')
    call("a", "profile", "map-resource", "staging", "deploy-preview", "db", resource["id"])
    call("a", "profile", "select", "staging")
    call("a", "secret", "bind", "deploy-preview", "db-password", "--profile", "staging", "--alias", "atlas/staging/db",
         "--provider", "infisical", "--provider-profile", "synthetic", "--method", "universal-auth",
         "--backend-project", "synthetic-backend", "--environment", "dev", "--path", "/db", "--key", "DATABASE_URL")
    base = document("a", "Deployment runbook", "# Deployment\n\n1. Review the candidate.\n2. Deploy the preview.\n")

    skill = root / "skill"
    skill.mkdir()
    (skill / "SKILL.md").write_text("---\nname: release-notes\nlicense: MIT\n---\n# Release notes\n\nInstalled version.\n")
    (skill / "template.md").write_text("# Candidate template\n")
    call("a", "skill", "add", "--share", "private", "--local", str(skill))
    call("a", "skill", "group", "set", "project:atlas-api", "release-notes")
    call("a", "sync", "enable", "--remote", REMOTE)
    preview = call("a", "sync", "preview")["previewId"]
    call("a", "sync", "init", "--preview", preview)

    call("b", "join", REMOTE, "--all-projects")
    call("b", "project", "bind", ".", "--project", "atlas-api")
    call("b", "config", "set", "timeoutSeconds", "90", "--layer", "device")
    call("b", "skill", "apply", "release-notes", "--target", "codex")
    installed = home / ".agents" / "skills" / "release-notes" / "SKILL.md"
    installed.write_text(installed.read_text() + "\nLocal drift for review.\n")

    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen(1)
    target = "%s:%d" % listener.getsockname()
    call("b", "resource", "route", resource["id"], "--endpoint", target)
    accepted = []
    def accept():
        listener.settimeout(10)
        try:
            conn, _ = listener.accept()
            accepted.append(True)
            conn.close()
        except socket.timeout:
            pass
    thread = threading.Thread(target=accept)
    thread.start()
    call("b", "resource", "check", resource["id"])
    thread.join()
    listener.close()
    assert accepted

    for name, summary in (("latency-report.json", "Latency report complete"), ("deployment.log", "Deployment log complete")):
        payload = checkout["b"] / name
        payload.write_text('{"status":"ok","synthetic":true}\n' if name.endswith("json") else "Synthetic deployment completed\n")
        call("b", "artifact", "add", name, "--project", "atlas-api", "--type", "text/plain", "--sensitivity", "internal",
             "--source-revision", "synthetic-revision", "--dirty=false", "--profile", "staging", "--tool", "deploy@synthetic",
             "--command", "deploy", "--exit-code", "0", "--summary", summary, "--completed", "synthetic-run")

    (skill / "SKILL.md").write_text("---\nname: release-notes\nlicense: MIT\n---\n# Release notes\n\nApproved candidate version.\n")
    (skill / "template.md").write_text("# Approved candidate template\n")
    call("a", "skill", "add", "--share", "private", "--local", str(skill))
    document("a", "Deployment runbook", "# Deployment\n\n1. Review the candidate.\n2. Deploy with the remote revision.\n", base)
    call("a", "sync")
    document("b", "Deployment runbook", "# Deployment\n\n1. Review the candidate.\n2. Deploy with the local revision.\n", base)
    call("b", "sync")
    status = call("b", "sync", "status")
    assert len(status["conflicts"]) == 1 and status["head"]
    return data["b"], {"confirmedHead": status["head"], "pending": status["pending"], "conflicts": 1,
                       "resource": resource, "task": task, "probeTarget": target, "skill": "release-notes",
                       "projects": ["atlas-api"], "profile": "staging", "artifactCount": 2}


def native_env(data_dir, home, endpoint, compact):
    env = {"PATH": os.environ["PATH"], "HOME": str(home), "CTXFERRY_DATA_DIR": str(data_dir),
           "MYGO_AUTOMATION": str(endpoint), "DISPLAY": ":0", "WAYLAND_DISPLAY": "wayland-0",
           "XDG_RUNTIME_DIR": "/mnt/wslg/runtime-dir", "LIBGL_ALWAYS_SOFTWARE": "1",
           "CTXFERRY_INFISICAL_ACCESS_TOKEN": SENTINEL}
    for key in ("GH_TOKEN", "CTXFERRY_TEST_API", "CTXFERRY_TEST_REMOTE"):
        if key in os.environ:
            env[key] = os.environ[key]
    if compact:
        env["CTXFERRY_DESKTOP_COMPACT"] = "1"
    return env


def open_native(binary, data_dir, home, endpoint, compact, ready="Home"):
    log_path = endpoint.with_suffix(".log")
    log = log_path.open("w")
    proc = subprocess.Popen([binary], env=native_env(data_dir, home, endpoint, compact), stdout=log, stderr=log)
    return proc, log


def capture_wide(binary, data_dir, home, root, images, states):
    endpoint = root / "wide.sock"
    proc, log = open_native(binary, data_dir, home, endpoint, False)
    window = Window(endpoint, images, states)
    try:
        window.label("Home")
        window.capture(root, "01-home", "Real Home: one active conflict, local queue evidence and one bound Project")
        window.click("Projects")
        window.label("atlas-api")
        window.capture(root, "02-projects", "Real subscribed Project list")
        window.click("Open Project atlas-api")
        window.label("Checkouts on this device")
        window.capture(root, "03-project-overview", "atlas-api overview with its real Checkout and staging Profile")
        window.click("Configuration")
        window.label("Effective Configuration")
        window.capture(root, "04-configuration", "Real effective timeoutSeconds and ordered layer contributions")
        window.click("Preview removal from Device…")
        window.label("Preview removal from Device")

        node = window.label("Confirms against preview token")
        window.call("act", id=node["id"], action="scrollIntoView")
        window.capture(root, "05-configuration-preview", "Unconfirmed removal of the Device override; actual preview token and effects")
        window.click("Cancel")
        window.click("Tasks")
        window.click("Open Task deploy-preview")
        window.label("Probe target · service port")
        window.capture(root, "06-task-probe", "Task slot mapped to postgres-primary with the exact local TCP target and observation timestamp")
        window.click("Open Resource postgres-primary")
        window.label("Route and observations")
        window.capture(root, "07-resource", "Resource route and per-environment observations returned from the Task")
        window.click("Back to Project")
        window.click("Open requirement")
        window.label("Resource match")
        window.capture(root, "08-credentials", "Real shared credential reference and independently unknown provider authentication")
        window.click("Catalog")
        window.label("Tools and records shared across Projects.")
        window.capture(root, "09-catalog", "Catalog directory with atlas-api scope")
        window.click("Open Skills")
        window.click("Preview update…")
        window.label("Installed version → current local file (drift)")
        window.capture(root, "10-skill-drift", "Blocked Skill update showing installed-to-local drift")
        window.click("View installed → candidate")
        window.capture(root, "11-skill-candidate", "The same file showing installed-to-candidate changes")
        window.click("Catalog")
        window.click("Open Run artifacts")
        window.label("Run record")
        window.capture(root, "12-artifacts", "Two real local run artifacts with provenance and full digests")
        window.click("Sync")
        window.click("Check remote")
        wait_for(lambda: any("private repository verified" in node.get("label", "") for node in window.nodes()))
        window.capture(root, "13-repository", "Confirmed repository history and a real reachability/privacy check")
        window.click_prefix("Queue · ")
        window.label("Publication queue")
        window.capture(root, "14-queue", "Real conflict and waiting records grouped by stable ID")
        window.click("Open comparison")
        window.label("Three-way comparison")
        window.click("Keep local version")
        window.capture(root, "15-conflict", "Unresolved real conflict with Keep local version preview and conflict token")
        window.click("Cancel")
        window.click("Backup")
        window.set_value("Backup destination", str(root / "wide-backup"))
        window.click("Review backup")
        node = window.label("Acknowledge plaintext backup exposure")
        window.call("act", id=node["id"], action="scrollIntoView")
        window.capture(root, "16-backup-review", "Real backup scope, exclusions and plaintext exposure before writing")
        window.click("Acknowledge plaintext backup exposure")
        window.click("Write backup")
        window.label("Backup written")
        window.capture(root, "17-backup-receipt", "Verified new backup and manifest receipt")
    finally:
        if proc.poll() is None:
            proc.terminate()
        proc.wait(timeout=10)
        log.close()


def capture_compact(binary, data_dir, home, root, images, states):
    endpoint = root / "compact.sock"
    proc, log = open_native(binary, data_dir, home, endpoint, True)
    window = Window(endpoint, images, states)
    try:
        window.label("Home")
        window.capture(root, "18-compact-home", "1024x680 native compact Home with icon rail")
        window.click("Projects")
        window.click("Open Project atlas-api")
        window.click("Configuration")
        window.click("Preview removal from Device…")
        node = window.label("Confirmation actions")
        window.call("act", id=node["id"], action="scrollIntoView")
        window.capture(root, "19-compact-configuration", "1024x680 Configuration preview with pinned token and confirmation")
        window.click("Cancel")
        window.click("Home")
        window.click("Queue · Conflicts")
        window.click("Open comparison")
        window.click("Keep local version")
        window.capture(root, "20-compact-conflict", "1024x680 conflict resolution with pinned conflict token and action")
        window.click("Cancel")
        window.click("Sync")
        window.click("Backup")
        window.set_value("Backup destination", str(root / "compact-backup"))
        window.click("Review backup")
        window.capture(root, "21-compact-backup", "1024x680 Backup review with pinned exposure acknowledgement and write")
    finally:
        if proc.poll() is None:
            proc.terminate()
        proc.wait(timeout=10)
        log.close()


def capture_readiness(binary, home, root, images, states):
    fresh = root / "fresh-data"
    endpoint = root / "readiness.sock"
    proc, log = open_native(binary, fresh, home, endpoint, False, "Welcome to CtxFerry")
    window = Window(endpoint, images, states)
    try:
        window.label("Welcome to CtxFerry")
        window.capture(root, "00-readiness", "Fresh isolated data root with real create and join choices")
    finally:
        if proc.poll() is None:
            proc.terminate()
        proc.wait(timeout=10)
        log.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("evidence_directory", type=Path, help="A new isolated directory under /tmp")
    parser.add_argument("--revision", default=run("git", "rev-parse", "HEAD", cwd=REPO))
    args = parser.parse_args()
    root = args.evidence_directory.resolve()
    assert str(root).startswith("/tmp/") and not root.exists(), root
    root.mkdir(mode=0o700)
    home = root / "home"
    home.mkdir()
    cli = root / "ctxferry"
    desktop = root / "desktop"
    run("go", "build", "-tags", "ctxferry_test", "-o", cli, "./cmd/ctxferry", cwd=REPO)
    run("go", "build", "-tags", "desktop_smoke,ctxferry_test", "-o", desktop, ".", cwd=DESKTOP)
    with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Metadata) as server:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            data_dir, fixture = cli_fixture(root, server.server_port, cli, home)
            os.environ.update(GH_TOKEN="synthetic-git", CTXFERRY_TEST_API=f"http://127.0.0.1:{server.server_port}",
                              CTXFERRY_TEST_REMOTE=str(root / "remote.git"))
            images, states = {}, {}
            capture_readiness(desktop, home, root, images, states)
            capture_wide(desktop, data_dir, home, root, images, states)
            capture_compact(desktop, data_dir, home, root, images, states)
        finally:
            server.shutdown()
            thread.join()
    manifest = {"implementationRevision": args.revision, "renderer": "Running MyGo native window via desktop_smoke automation",
                "isolatedDataDir": str(data_dir), "fixture": fixture, "images": images}
    (root / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    main()
