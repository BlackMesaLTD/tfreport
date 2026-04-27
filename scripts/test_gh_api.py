#!/usr/bin/env python3
"""Smoke tests for scripts/gh_api.py using mocked urllib.request.urlopen."""
from __future__ import annotations

import json
import os
import sys
import unittest
from io import BytesIO
from unittest.mock import MagicMock, patch

sys.path.insert(0, os.path.dirname(__file__))

import gh_api  # noqa: E402


def mock_response(payload: object) -> MagicMock:
    m = MagicMock()
    m.__enter__ = MagicMock(return_value=m)
    m.__exit__ = MagicMock(return_value=False)
    m.read = MagicMock(return_value=json.dumps(payload).encode("utf-8"))
    return m


class GitAPITests(unittest.TestCase):
    def setUp(self) -> None:
        self.api = gh_api.GitAPI(token="TEST", repo="octo/repo", pr_number="42")

    def test_get_pr_body_returns_body(self) -> None:
        with patch("urllib.request.urlopen", return_value=mock_response({"body": "Hello\r\nWorld"})):
            got = self.api.get_pr_body()
        self.assertEqual(got, "Hello\nWorld")

    def test_get_pr_body_empty(self) -> None:
        with patch("urllib.request.urlopen", return_value=mock_response({"body": None})):
            got = self.api.get_pr_body()
        self.assertEqual(got, "")

    def test_update_pr_body_sends_patch(self) -> None:
        captured: dict[str, object] = {}

        def fake_urlopen(req, timeout=None):  # noqa: ANN001
            captured["method"] = req.get_method()
            captured["url"] = req.full_url
            captured["data"] = json.loads(req.data.decode("utf-8"))
            return mock_response({})

        with patch("urllib.request.urlopen", side_effect=fake_urlopen):
            self.api.update_pr_body("new text")

        self.assertEqual(captured["method"], "PATCH")
        self.assertIn("/pulls/42", captured["url"])
        self.assertEqual(captured["data"], {"body": "new text"})

    def test_find_sticky_comment_matches_tag(self) -> None:
        comments = [
            {"id": 1, "body": "hello world"},
            {"id": 2, "body": "<!-- TFREPORT --> actual content"},
        ]
        with patch("urllib.request.urlopen", return_value=mock_response(comments)):
            got = self.api.find_sticky_comment("TFREPORT")
        self.assertIsNotNone(got)
        self.assertEqual(got["id"], 2)

    def test_find_sticky_comment_none(self) -> None:
        with patch("urllib.request.urlopen", return_value=mock_response([])):
            got = self.api.find_sticky_comment("MISSING")
        self.assertIsNone(got)

    def test_upsert_sticky_creates_new(self) -> None:
        calls: list[str] = []

        def fake_urlopen(req, timeout=None):  # noqa: ANN001
            calls.append(req.get_method())
            # First call: list comments (empty). Second: POST new.
            if req.get_method() == "GET":
                return mock_response([])
            return mock_response({"id": 99})

        with patch("urllib.request.urlopen", side_effect=fake_urlopen):
            self.api.upsert_sticky_comment("MARKER", "body text")

        self.assertEqual(calls, ["GET", "POST"])


class SpliceTests(unittest.TestCase):
    def test_splice_replaces_existing(self) -> None:
        old = "before\n<!-- BEGIN_M -->\nold inner\n<!-- END_M -->\nafter"
        out = gh_api.splice_pr_body(old, "new inner", "M")
        self.assertIn("new inner", out)
        self.assertNotIn("old inner", out)
        self.assertIn("before", out)
        self.assertIn("after", out)

    def test_splice_appends_when_markers_missing(self) -> None:
        out = gh_api.splice_pr_body("pre-existing body text", "snippet", "M")
        self.assertIn("<!-- BEGIN_M -->", out)
        self.assertIn("<!-- END_M -->", out)
        self.assertIn("snippet", out)

    def test_splice_handles_empty_body(self) -> None:
        out = gh_api.splice_pr_body("", "snippet", "M")
        self.assertIn("<!-- BEGIN_M -->", out)
        self.assertIn("snippet", out)


class MarkerStripTests(unittest.TestCase):
    def test_strips_matching_prefix(self) -> None:
        body = "<!-- TFREPORT -->\n- [x] content"
        self.assertEqual(
            gh_api.strip_marker_prefix(body, "TFREPORT"),
            "- [x] content",
        )

    def test_passthrough_when_no_prefix(self) -> None:
        body = "- [x] content without the tag"
        self.assertEqual(gh_api.strip_marker_prefix(body, "TFREPORT"), body)

    def test_passthrough_on_different_marker(self) -> None:
        body = "<!-- OTHER -->\n- [x] content"
        # Tag present but for a different marker name → leave it.
        self.assertEqual(gh_api.strip_marker_prefix(body, "TFREPORT"), body)

    def test_only_strips_leading_occurrence(self) -> None:
        body = "<!-- TFREPORT -->\n- [x] content\n<!-- TFREPORT -->\n trailing"
        got = gh_api.strip_marker_prefix(body, "TFREPORT")
        self.assertTrue(got.startswith("- [x] content"))
        # The trailing occurrence is preserved verbatim — we only strip a prefix.
        self.assertIn("<!-- TFREPORT -->\n trailing", got)


class LabelMethodTests(unittest.TestCase):
    """Unit tests for the GitAPI label primitives (warm/cold upsert, attach,
    detach, list). Each test captures the (method, url, body) sequence and
    asserts on shape, not transport details."""

    def setUp(self) -> None:
        self.api = gh_api.GitAPI(token="TEST", repo="octo/repo", pr_number="42")

    def _capture(self, responder):
        """Build a fake urlopen that records calls and delegates to `responder`
        for the response body. Returns (calls list, fake fn)."""
        calls: list[dict[str, object]] = []

        def fake_urlopen(req, timeout=None):  # noqa: ANN001
            entry = {
                "method": req.get_method(),
                "url": req.full_url,
                "body": json.loads(req.data.decode("utf-8")) if req.data else None,
            }
            calls.append(entry)
            return responder(entry)

        return calls, fake_urlopen

    def test_upsert_repo_label_warm_path_uses_patch(self) -> None:
        """PATCH 200 = label exists; no POST fallback fires."""
        def responder(entry):
            if entry["method"] == "PATCH":
                return mock_response({"name": entry["body"].get("name") or "x"})
            self.fail(f"unexpected call: {entry}")

        calls, fake = self._capture(responder)
        with patch("urllib.request.urlopen", side_effect=fake):
            self.api.upsert_repo_label("[ + ] sub-eastus", "00ff00", "Changes Detected in Account. [tfreport]")

        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["method"], "PATCH")
        self.assertIn("/labels/", calls[0]["url"])
        self.assertEqual(calls[0]["body"], {"color": "00ff00", "description": "Changes Detected in Account. [tfreport]"})

    def test_upsert_repo_label_cold_path_falls_back_to_post(self) -> None:
        """PATCH 404 -> POST creates the label."""
        from urllib.error import HTTPError
        from io import BytesIO

        def responder(entry):
            if entry["method"] == "PATCH":
                raise HTTPError(entry["url"], 404, "Not Found", {}, BytesIO(b'{"message":"Not Found"}'))
            if entry["method"] == "POST":
                return mock_response({"name": entry["body"]["name"]})
            self.fail(f"unexpected call: {entry}")

        calls, fake = self._capture(responder)
        with patch("urllib.request.urlopen", side_effect=fake):
            self.api.upsert_repo_label("[ + ] sub-eastus", "00ff00", "Changes Detected in Account. [tfreport]")

        self.assertEqual([c["method"] for c in calls], ["PATCH", "POST"])
        self.assertEqual(calls[1]["body"], {
            "name": "[ + ] sub-eastus",
            "color": "00ff00",
            "description": "Changes Detected in Account. [tfreport]",
        })

    def test_attach_pr_label_uses_post(self) -> None:
        calls, fake = self._capture(lambda e: mock_response([{"name": e["body"]["labels"][0]}]))
        with patch("urllib.request.urlopen", side_effect=fake):
            self.api.attach_pr_label("[ + ] sub-eastus")
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["method"], "POST")
        self.assertIn("/issues/42/labels", calls[0]["url"])
        self.assertEqual(calls[0]["body"], {"labels": ["[ + ] sub-eastus"]})

    def test_detach_pr_label_uses_delete(self) -> None:
        calls, fake = self._capture(lambda e: mock_response(None))
        with patch("urllib.request.urlopen", side_effect=fake):
            self.api.detach_pr_label("[ + ] sub-eastus")
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["method"], "DELETE")
        self.assertIn("/issues/42/labels/", calls[0]["url"])

    def test_detach_tolerates_404(self) -> None:
        from urllib.error import HTTPError
        from io import BytesIO

        def responder(entry):
            raise HTTPError(entry["url"], 404, "Not Found", {}, BytesIO(b'{}'))

        _, fake = self._capture(responder)
        with patch("urllib.request.urlopen", side_effect=fake):
            self.api.detach_pr_label("missing")  # should not raise

    def test_list_pr_labels_pages(self) -> None:
        page1 = [{"name": f"L{i}", "description": ""} for i in range(100)]
        page2 = [{"name": "L100", "description": ""}]

        responses = iter([mock_response(page1), mock_response(page2)])

        def fake_urlopen(req, timeout=None):  # noqa: ANN001
            return next(responses)

        with patch("urllib.request.urlopen", side_effect=fake_urlopen):
            got = self.api.list_pr_labels()
        self.assertEqual(len(got), 101)


class DoLabelsTests(unittest.TestCase):
    """End-to-end behaviour of the do_labels reconciliation handler:
    warm/cold upsert + attach + stale-removal, all driven by a manifest file."""

    def setUp(self) -> None:
        import tempfile
        self.tmpdir = tempfile.mkdtemp()

    def tearDown(self) -> None:
        import shutil
        shutil.rmtree(self.tmpdir, ignore_errors=True)

    def _write_manifest(self, payload: object) -> str:
        path = os.path.join(self.tmpdir, "labels.json")
        with open(path, "w", encoding="utf-8") as f:
            json.dump(payload, f)
        return path

    def _args(self, manifest: str) -> "argparse.Namespace":
        import argparse as _argparse
        return _argparse.Namespace(
            github_token="TEST",
            pr_number="42",
            manifest=manifest,
        )

    def _fake_github(self, current_pr_labels=None, repo_labels=None):
        """Return a urlopen mock that simulates a tiny GitHub repo.

        repo_labels: dict[name -> True] whose keys exist (PATCH 200);
                     missing names trigger 404 on PATCH so do_labels POSTs.
        current_pr_labels: list of labels currently on the PR (used by GET
                           /issues/{pr}/labels). Each entry must be a dict
                           with at least 'name' and 'description'.
        """
        from urllib.error import HTTPError
        from io import BytesIO

        repo_labels = dict(repo_labels or {})
        current_pr_labels = list(current_pr_labels or [])
        calls: list[dict[str, object]] = []

        def fake_urlopen(req, timeout=None):  # noqa: ANN001
            method = req.get_method()
            url = req.full_url
            body = json.loads(req.data.decode("utf-8")) if req.data else None
            calls.append({"method": method, "url": url, "body": body})

            # GET PR labels (paginated)
            if method == "GET" and "/issues/42/labels" in url:
                # Simple single-page response.
                return mock_response(current_pr_labels)

            # PATCH repo label
            if method == "PATCH" and "/labels/" in url and "/issues/" not in url:
                # Extract name from URL path.
                from urllib.parse import unquote
                name = unquote(url.rsplit("/", 1)[-1])
                if name not in repo_labels:
                    raise HTTPError(url, 404, "Not Found", {}, BytesIO(b'{}'))
                return mock_response({"name": name})

            # POST attach to PR (must be checked BEFORE bare /labels POST
            # since both URLs end in /labels).
            if method == "POST" and "/issues/42/labels" in url:
                return mock_response([{"name": body["labels"][0]}])

            # POST repo label (cold path)
            if method == "POST" and url.endswith("/labels"):
                repo_labels[body["name"]] = True
                return mock_response({"name": body["name"]})

            # DELETE PR label
            if method == "DELETE" and "/issues/42/labels/" in url:
                return mock_response(None)

            self.fail(f"unhandled call: {method} {url}")

        return calls, fake_urlopen

    def test_happy_path_warm_upsert_then_attach(self) -> None:
        """Manifest with one entry whose label already exists in the repo:
        PATCH succeeds, attach fires, no stale-removal candidates."""
        manifest = self._write_manifest([
            {"name": "[ + ] sub-eastus", "color": "00ff00", "description": "Changes Detected in Account. [tfreport]"}
        ])
        calls, fake = self._fake_github(
            repo_labels={"[ + ] sub-eastus": True},
            current_pr_labels=[
                {"name": "[ + ] sub-eastus", "description": "Changes Detected in Account. [tfreport]"}
            ],
        )
        with patch("urllib.request.urlopen", side_effect=fake), \
             patch.dict(os.environ, {"GITHUB_REPOSITORY": "octo/repo"}):
            rc = gh_api.do_labels(self._args(manifest))
        self.assertEqual(rc, 0)
        seq = [(c["method"], c["url"].rsplit("/", 1)[-1] if "/" in c["url"] else "") for c in calls]
        # Ordering: PATCH label, POST attach, GET PR labels.
        self.assertEqual(seq[0][0], "PATCH")
        self.assertEqual(seq[1][0], "POST")
        self.assertEqual(seq[1][1], "labels")  # /issues/42/labels
        self.assertEqual(seq[2][0], "GET")

    def test_cold_path_404_then_post(self) -> None:
        """Manifest entry whose label does NOT exist: PATCH 404 -> POST -> attach."""
        manifest = self._write_manifest([
            {"name": "[ + ] sub-new", "color": "00ff00", "description": "Changes Detected in Account. [tfreport]"}
        ])
        calls, fake = self._fake_github(
            repo_labels={},  # nothing exists yet
            current_pr_labels=[],
        )
        with patch("urllib.request.urlopen", side_effect=fake), \
             patch.dict(os.environ, {"GITHUB_REPOSITORY": "octo/repo"}):
            rc = gh_api.do_labels(self._args(manifest))
        self.assertEqual(rc, 0)
        methods = [c["method"] for c in calls]
        # PATCH (404) -> POST create -> POST attach -> GET list
        self.assertEqual(methods, ["PATCH", "POST", "POST", "GET"])

    def test_stale_removal_drops_marker_stamped_not_in_manifest(self) -> None:
        """A marker-stamped PR label not in the manifest gets DELETED."""
        manifest = self._write_manifest([
            {"name": "[ + ] sub-keep", "color": "00ff00", "description": "Changes Detected in Account. [tfreport]"}
        ])
        calls, fake = self._fake_github(
            repo_labels={"[ + ] sub-keep": True, "[ - ] sub-stale": True},
            current_pr_labels=[
                {"name": "[ + ] sub-keep", "description": "Changes Detected in Account. [tfreport]"},
                {"name": "[ - ] sub-stale", "description": "Changes Detected in Account. [tfreport]"},
                {"name": "human-label", "description": "untouched"},
            ],
        )
        with patch("urllib.request.urlopen", side_effect=fake), \
             patch.dict(os.environ, {"GITHUB_REPOSITORY": "octo/repo"}):
            rc = gh_api.do_labels(self._args(manifest))
        self.assertEqual(rc, 0)
        deletes = [c for c in calls if c["method"] == "DELETE"]
        self.assertEqual(len(deletes), 1, f"expected exactly one DELETE, got {deletes}")
        self.assertIn("sub-stale", deletes[0]["url"])

    def test_empty_manifest_removes_all_marked(self) -> None:
        """An empty manifest still triggers reconciliation: every marker-
        stamped PR label gets DELETED."""
        manifest = self._write_manifest([])
        calls, fake = self._fake_github(
            current_pr_labels=[
                {"name": "[ + ] a", "description": "Changes Detected in Account. [tfreport]"},
                {"name": "[ - ] b", "description": "Changes Detected in Account. [tfreport]"},
            ],
        )
        with patch("urllib.request.urlopen", side_effect=fake), \
             patch.dict(os.environ, {"GITHUB_REPOSITORY": "octo/repo"}):
            rc = gh_api.do_labels(self._args(manifest))
        self.assertEqual(rc, 0)
        deletes = [c for c in calls if c["method"] == "DELETE"]
        self.assertEqual(len(deletes), 2)

    def test_unmarked_labels_left_alone(self) -> None:
        """Labels whose description does NOT end with the marker are
        considered foreign and never deleted."""
        manifest = self._write_manifest([])
        calls, fake = self._fake_github(
            current_pr_labels=[
                {"name": "manual-needs-review", "description": "Owned by humans"},
                {"name": "ci/build-failed", "description": ""},
            ],
        )
        with patch("urllib.request.urlopen", side_effect=fake), \
             patch.dict(os.environ, {"GITHUB_REPOSITORY": "octo/repo"}):
            rc = gh_api.do_labels(self._args(manifest))
        self.assertEqual(rc, 0)
        deletes = [c for c in calls if c["method"] == "DELETE"]
        self.assertEqual(deletes, [])

    def test_no_pr_context_warn_and_skip(self) -> None:
        """Without a PR number and without GITHUB_EVENT_PATH, the handler
        emits a warning and exits 0 without making any API calls."""
        manifest = self._write_manifest([
            {"name": "x", "color": "00ff00", "description": "y [tfreport]"}
        ])
        import argparse as _argparse
        args = _argparse.Namespace(github_token="TEST", pr_number="", manifest=manifest)
        with patch("urllib.request.urlopen", side_effect=AssertionError("should not be called")), \
             patch.dict(os.environ, {"GITHUB_REPOSITORY": "octo/repo", "GITHUB_EVENT_PATH": ""}, clear=False):
            os.environ.pop("GITHUB_EVENT_PATH", None)
            rc = gh_api.do_labels(args)
        self.assertEqual(rc, 0)

    def test_missing_token_errors(self) -> None:
        manifest = self._write_manifest([])
        import argparse as _argparse
        args = _argparse.Namespace(github_token="", pr_number="42", manifest=manifest)
        rc = gh_api.do_labels(args)
        self.assertEqual(rc, 1)

    def test_missing_manifest_errors(self) -> None:
        import argparse as _argparse
        args = _argparse.Namespace(github_token="TEST", pr_number="42", manifest="/nonexistent/path.json")
        rc = gh_api.do_labels(args)
        self.assertEqual(rc, 1)


if __name__ == "__main__":
    unittest.main()
