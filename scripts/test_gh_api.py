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


if __name__ == "__main__":
    unittest.main()
