#!/usr/bin/env python3
"""End-to-end integration test for the labels reconciliation pipeline.

Stands up a minimal stub of the GitHub REST surface (PATCH/POST /labels,
GET/POST /issues/{pr}/labels, DELETE /issues/{pr}/labels/{name}) on a local
loopback port. Runs gh_api.py --labels against fixture manifests, plus one
full pipeline pass that drives tfreport --target labels into gh_api.py to
prove the wire contract holds end-to-end.

No external dependencies: stdlib http.server + threading only.

Five scenarios covered (REQ-label-management-003):
  1. Happy path apply
  2. Stale-removal of an orphaned marker-stamped label
  3. Empty manifest removes all marked labels
  4. JIT cold path (label not in repo -> POST after PATCH 404)
  5. JIT warm path (label exists -> PATCH only)
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import unquote, urlparse

sys.path.insert(0, os.path.dirname(__file__))
import gh_api  # noqa: E402

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


class StubGithubState:
    """Mutable backing store for the stub server. Tests inspect this after
    each run to assert on the resulting (repo_labels, pr_labels, calls)."""

    def __init__(self, *, repo_labels=None, pr_labels=None):
        self.repo_labels: dict[str, dict] = dict(repo_labels or {})
        self.pr_labels: list[dict] = list(pr_labels or [])
        self.calls: list[dict] = []
        self.lock = threading.Lock()


def make_handler(state: StubGithubState):
    """Build a BaseHTTPRequestHandler subclass closed over `state`."""

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, fmt, *args):  # silence noise on stderr
            pass

        def _record(self, method: str, body):
            with state.lock:
                state.calls.append({
                    "method": method,
                    "path": self.path,
                    "body": body,
                })

        def _send_json(self, code: int, payload):
            data = json.dumps(payload).encode("utf-8")
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def _read_body(self):
            length = int(self.headers.get("Content-Length") or 0)
            if not length:
                return None
            return json.loads(self.rfile.read(length).decode("utf-8"))

        def do_GET(self):  # noqa: N802
            self._record("GET", None)
            parsed = urlparse(self.path)
            if "/issues/42/labels" in parsed.path:
                self._send_json(200, state.pr_labels)
                return
            self._send_json(404, {"message": "not found"})

        def do_PATCH(self):  # noqa: N802
            body = self._read_body()
            self._record("PATCH", body)
            parsed = urlparse(self.path)
            if "/labels/" in parsed.path and "/issues/" not in parsed.path:
                name = unquote(parsed.path.rsplit("/", 1)[-1])
                if name not in state.repo_labels:
                    self._send_json(404, {"message": "not found"})
                    return
                state.repo_labels[name].update({"color": body["color"], "description": body["description"]})
                self._send_json(200, {"name": name, **state.repo_labels[name]})
                return
            self._send_json(404, {"message": "not found"})

        def do_POST(self):  # noqa: N802
            body = self._read_body()
            self._record("POST", body)
            parsed = urlparse(self.path)
            # Attach to PR (longer/more specific path checked first).
            if parsed.path.endswith("/issues/42/labels"):
                for name in body.get("labels", []):
                    # Idempotent: skip duplicates.
                    if not any(lbl["name"] == name for lbl in state.pr_labels):
                        meta = state.repo_labels.get(name, {})
                        state.pr_labels.append({
                            "name": name,
                            "description": meta.get("description", ""),
                        })
                self._send_json(200, [{"name": n} for n in body.get("labels", [])])
                return
            # Create repo label (cold path).
            if parsed.path.endswith("/labels"):
                state.repo_labels[body["name"]] = {
                    "color": body["color"],
                    "description": body["description"],
                }
                self._send_json(201, {"name": body["name"]})
                return
            self._send_json(404, {"message": "not found"})

        def do_DELETE(self):  # noqa: N802
            self._record("DELETE", None)
            parsed = urlparse(self.path)
            if "/issues/42/labels/" in parsed.path:
                name = unquote(parsed.path.rsplit("/", 1)[-1])
                state.pr_labels = [lbl for lbl in state.pr_labels if lbl["name"] != name]
                self._send_json(204, {})
                return
            self._send_json(404, {"message": "not found"})

    return Handler


class StubServer:
    """Threaded local HTTP stub. Use as a context manager."""

    def __init__(self, state: StubGithubState):
        self.state = state
        self.httpd = HTTPServer(("127.0.0.1", 0), make_handler(state))
        self.thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)

    @property
    def base_url(self) -> str:
        host, port = self.httpd.server_address
        return f"http://{host}:{port}"

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *exc):
        self.httpd.shutdown()
        self.httpd.server_close()
        self.thread.join(timeout=2)


def run_gh_api_labels(manifest_path: str, base_url: str) -> int:
    """Invoke gh_api.py --labels with API_BASE redirected to the stub."""
    env = os.environ.copy()
    env["GITHUB_REPOSITORY"] = "octo/repo"
    scripts_dir = os.path.dirname(os.path.abspath(__file__))
    # Tiny shim: import gh_api, override API_BASE, dispatch to main().
    code = (
        f"import sys; sys.path.insert(0, {scripts_dir!r});"
        f"import gh_api; gh_api.GitAPI.API_BASE={base_url!r};"
        "sys.exit(gh_api.main())"
    )
    cmd = [
        sys.executable, "-c", code,
        "--labels", "--manifest", manifest_path,
        "--github-token", "TEST", "--pr-number", "42",
    ]
    proc = subprocess.run(cmd, env=env, cwd=REPO_ROOT, capture_output=True, text=True, timeout=20)
    if proc.returncode != 0:
        print("stdout:", proc.stdout, file=sys.stderr)
        print("stderr:", proc.stderr, file=sys.stderr)
    return proc.returncode


def _write_manifest(payload):
    fd, path = tempfile.mkstemp(suffix=".json")
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump(payload, f)
    return path


MARKED_DESC = "Changes Detected in Account. [tfreport]"


class E2EReconciliationTests(unittest.TestCase):
    def test_1_happy_path_apply(self):
        manifest = _write_manifest([
            {"name": "[ + ] sub-eastus", "color": "00ff00", "description": MARKED_DESC},
        ])
        try:
            state = StubGithubState()
            with StubServer(state) as srv:
                rc = run_gh_api_labels(manifest, srv.base_url)
            self.assertEqual(rc, 0)
            # Repo created label, PR has it attached.
            self.assertIn("[ + ] sub-eastus", state.repo_labels)
            names = [lbl["name"] for lbl in state.pr_labels]
            self.assertIn("[ + ] sub-eastus", names)
        finally:
            os.unlink(manifest)

    def test_2_stale_removal_orphaned_marker_stamped(self):
        manifest = _write_manifest([
            {"name": "[ + ] keep", "color": "00ff00", "description": MARKED_DESC},
        ])
        try:
            state = StubGithubState(
                repo_labels={
                    "[ + ] keep": {"color": "00ff00", "description": MARKED_DESC},
                    "[ - ] stale": {"color": "d73a4a", "description": MARKED_DESC},
                },
                pr_labels=[
                    {"name": "[ + ] keep", "description": MARKED_DESC},
                    {"name": "[ - ] stale", "description": MARKED_DESC},
                    {"name": "human-untouched", "description": "owned by humans"},
                ],
            )
            with StubServer(state) as srv:
                rc = run_gh_api_labels(manifest, srv.base_url)
            self.assertEqual(rc, 0)
            names = sorted(lbl["name"] for lbl in state.pr_labels)
            self.assertEqual(names, ["[ + ] keep", "human-untouched"])
        finally:
            os.unlink(manifest)

    def test_3_empty_manifest_removes_all_marked(self):
        manifest = _write_manifest([])
        try:
            state = StubGithubState(
                pr_labels=[
                    {"name": "[ + ] a", "description": MARKED_DESC},
                    {"name": "[ - ] b", "description": MARKED_DESC},
                    {"name": "human", "description": "owned by humans"},
                ],
            )
            with StubServer(state) as srv:
                rc = run_gh_api_labels(manifest, srv.base_url)
            self.assertEqual(rc, 0)
            names = [lbl["name"] for lbl in state.pr_labels]
            self.assertEqual(names, ["human"])
        finally:
            os.unlink(manifest)

    def test_4_jit_cold_path_creates_then_attaches(self):
        manifest = _write_manifest([
            {"name": "[ ~ ] sub-new", "color": "ffbf00", "description": MARKED_DESC},
        ])
        try:
            state = StubGithubState()  # nothing exists yet
            with StubServer(state) as srv:
                rc = run_gh_api_labels(manifest, srv.base_url)
                methods = [c["method"] for c in state.calls]
            self.assertEqual(rc, 0)
            # PATCH (404) -> POST create -> POST attach -> GET list
            self.assertEqual(methods[:4], ["PATCH", "POST", "POST", "GET"])
            self.assertIn("[ ~ ] sub-new", state.repo_labels)
        finally:
            os.unlink(manifest)

    def test_5_jit_warm_path_patches_only(self):
        manifest = _write_manifest([
            {"name": "[ ~ ] sub-existing", "color": "ffbf00", "description": MARKED_DESC},
        ])
        try:
            state = StubGithubState(
                repo_labels={
                    "[ ~ ] sub-existing": {"color": "ededed", "description": "old desc"},
                },
            )
            with StubServer(state) as srv:
                rc = run_gh_api_labels(manifest, srv.base_url)
                methods = [c["method"] for c in state.calls]
            self.assertEqual(rc, 0)
            # PATCH (200) -> POST attach -> GET list  (no POST /labels create)
            self.assertEqual(methods[:3], ["PATCH", "POST", "GET"])
            # Color and description got updated.
            self.assertEqual(state.repo_labels["[ ~ ] sub-existing"]["color"], "ffbf00")
            self.assertEqual(state.repo_labels["[ ~ ] sub-existing"]["description"], MARKED_DESC)
        finally:
            os.unlink(manifest)


class E2EFullPipelineTest(unittest.TestCase):
    """One test that drives `tfreport --target labels` against a real plan
    fixture, then feeds the resulting manifest into gh_api.py --labels."""

    def test_full_pipeline_plan_to_pr(self):
        plan_fixture = os.path.join(REPO_ROOT, "testdata", "medium_plan.json")
        if not os.path.isfile(plan_fixture):
            self.skipTest("medium_plan.json fixture not available")

        manifest_fd, manifest_path = tempfile.mkstemp(suffix=".json")
        os.close(manifest_fd)
        try:
            # Render manifest via go run.
            render = subprocess.run(
                [
                    "go", "run", "./cmd/tfreport",
                    "--plan-file", plan_fixture,
                    "--label", "sub-eastus",
                    "--target", "labels",
                ],
                cwd=REPO_ROOT,
                capture_output=True,
                text=True,
                timeout=120,
            )
            self.assertEqual(render.returncode, 0, f"tfreport failed: {render.stderr}")
            with open(manifest_path, "w", encoding="utf-8") as f:
                f.write(render.stdout)

            # Confirm we got a non-empty array.
            with open(manifest_path, encoding="utf-8") as f:
                manifest = json.load(f)
            self.assertIsInstance(manifest, list)
            self.assertGreaterEqual(len(manifest), 1)
            for spec in manifest:
                self.assertTrue(spec["description"].endswith(" [tfreport]"))
                self.assertIn(spec["color"], {"d73a4a", "ffbf00", "00ff00"})

            # Apply against the stub.
            state = StubGithubState()
            with StubServer(state) as srv:
                rc = run_gh_api_labels(manifest_path, srv.base_url)
            self.assertEqual(rc, 0)
            attached = sorted(lbl["name"] for lbl in state.pr_labels)
            expected = sorted(spec["name"] for spec in manifest)
            self.assertEqual(attached, expected)
        finally:
            if os.path.exists(manifest_path):
                os.unlink(manifest_path)


if __name__ == "__main__":
    unittest.main()
