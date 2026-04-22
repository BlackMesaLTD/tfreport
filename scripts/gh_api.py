#!/usr/bin/env python3
"""GitHub API helper for tfreport CI workflows.

Single stdlib-only script that covers three shapes:

  --send         Push a rendered tfreport snippet to one or more destinations:
                 step summary, PR body (marker-splice), and/or sticky PR
                 comment. Replaces the former scripts/send-report.py.
  --fetch-body   Emit the current PR body (full text, unmodified) to stdout
                 or --output path. Intended to feed the tfreport CLI's
                 --previous-body-file flag so state-preservation regions can
                 carry forward across renders.
  --fetch-comment  Emit the body of the sticky PR comment identified by
                   --marker. Empty string when no such comment exists.

Shape mirrors networks-azure/bin/git_api.py: class GitAPI wrapping a
request_composer transport, exposed via a thin CLI here.

PR context is auto-detected from $GITHUB_EVENT_PATH unless --pr-number
overrides. Repository is taken from $GITHUB_REPOSITORY. GitHub token is
taken from --github-token (required for any REST call).
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.request
from typing import Any


# ---------------------------------------------------------------------------
# Transport + REST wrapper
# ---------------------------------------------------------------------------


class GitAPI:
    """Thin wrapper around the GitHub REST API.

    One instance is bound to a specific (owner, repo, pr_number). The
    request_composer method is the single HTTPS transport — every higher-
    level method funnels through it so auth, headers, and error handling
    live in exactly one place.
    """

    API_BASE = "https://api.github.com"

    def __init__(self, token: str, repo: str, pr_number: str | None = None, timeout: int = 10):
        self.token = token
        self.repo = repo  # "owner/repo"
        self.pr_number = pr_number
        self.timeout = timeout
        self.repo_url = f"{self.API_BASE}/repos/{repo}"

    # -- Transport ----------------------------------------------------------

    def request_composer(
        self,
        url: str,
        method: str = "GET",
        data: dict[str, Any] | None = None,
    ) -> Any:
        """Execute a single REST call and return the parsed JSON response.

        Errors are raised as RuntimeError with the GitHub-provided detail.
        Callers decide how loudly to fail.
        """
        req = urllib.request.Request(url, method=method)
        req.add_header("Authorization", f"Bearer {self.token}")
        req.add_header("Accept", "application/vnd.github+json")
        req.add_header("X-GitHub-Api-Version", "2022-11-28")
        if data is not None:
            req.add_header("Content-Type", "application/json")
            req.data = json.dumps(data).encode("utf-8")
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as r:  # noqa: S310 (trusted URL)
                raw = r.read()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as e:
            detail = e.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"GitHub API {method} {url} failed: {e.code} {detail}") from e

    # -- PR body ------------------------------------------------------------

    def _require_pr(self) -> str:
        if not self.pr_number:
            raise RuntimeError("PR number not set — required for this operation")
        return self.pr_number

    def get_pr(self) -> dict[str, Any]:
        """Fetch the raw PR object."""
        pr = self._require_pr()
        return self.request_composer(f"{self.repo_url}/pulls/{pr}")

    def get_pr_body(self) -> str:
        """Return the current PR body (CRLF stripped). Empty string when absent."""
        pr = self.get_pr()
        return (pr.get("body") or "").replace("\r", "")

    def update_pr_body(self, body: str) -> None:
        """Overwrite the PR body wholesale."""
        pr = self._require_pr()
        self.request_composer(f"{self.repo_url}/pulls/{pr}", method="PATCH", data={"body": body})

    # -- Sticky comments ----------------------------------------------------

    def find_sticky_comment(self, marker: str) -> dict[str, Any] | None:
        """Scan PR comments for one whose body starts with the marker tag
        `<!-- <marker> -->`. Returns the full comment dict or None."""
        pr = self._require_pr()
        tag = f"<!-- {marker} -->"
        page = 1
        while True:
            comments = self.request_composer(
                f"{self.repo_url}/issues/{pr}/comments?per_page=100&page={page}"
            ) or []
            if not comments:
                return None
            for c in comments:
                if (c.get("body") or "").startswith(tag):
                    return c
            if len(comments) < 100:
                return None
            page += 1

    def upsert_sticky_comment(self, marker: str, body: str) -> None:
        """Edit the sticky comment matching marker if present; else post a new one."""
        pr = self._require_pr()
        tag = f"<!-- {marker} -->"
        new_body = f"{tag}\n{body}\n"
        existing = self.find_sticky_comment(marker)
        if existing:
            if (existing.get("body") or "").replace("\r", "") == new_body:
                return
            self.request_composer(
                f"{self.repo_url}/issues/comments/{existing['id']}",
                method="PATCH",
                data={"body": new_body},
            )
        else:
            self.request_composer(
                f"{self.repo_url}/issues/{pr}/comments",
                method="POST",
                data={"body": new_body},
            )


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def resolve_pr_number(explicit: str) -> str | None:
    if explicit:
        return explicit
    event_path = os.environ.get("GITHUB_EVENT_PATH") or ""
    if not event_path or not os.path.isfile(event_path):
        return None
    with open(event_path, encoding="utf-8") as f:
        event = json.load(f)
    pr = event.get("pull_request") or {}
    issue = event.get("issue") or {}
    num = pr.get("number") or issue.get("number")
    return str(num) if num else None


def load_snippet(path: str) -> str:
    if not os.path.isfile(path):
        print(f"::error::content-file not found: {path}", file=sys.stderr)
        sys.exit(1)
    with open(path, encoding="utf-8") as f:
        return f.read().strip()


def write_output(data: str, output: str) -> None:
    if output == "-" or output == "":
        sys.stdout.write(data)
        return
    with open(output, "w", encoding="utf-8") as f:
        f.write(data)


def splice_pr_body(existing: str, snippet: str, marker: str) -> str:
    begin = f"<!-- BEGIN_{marker} -->"
    end = f"<!-- END_{marker} -->"
    new_section = f"{begin}\n{snippet}\n{end}"
    pattern = re.compile(re.escape(begin) + r".*?" + re.escape(end), re.DOTALL)
    if pattern.search(existing):
        return pattern.sub(new_section, existing)
    if existing:
        return existing.rstrip() + "\n\n" + new_section + "\n"
    return new_section + "\n"


def do_send(args: argparse.Namespace) -> int:
    snippet = load_snippet(args.content_file)

    # Step summary has no API, no auth.
    if args.step_summary:
        path = os.environ.get("GITHUB_STEP_SUMMARY") or ""
        if not path:
            print("::warning::step-summary requested but $GITHUB_STEP_SUMMARY is not set", file=sys.stderr)
        else:
            with open(path, "a", encoding="utf-8") as f:
                f.write("\n" + snippet + "\n")
            print("step-summary: appended", file=sys.stderr)

    if not args.pr_body_marker and not args.pr_comment_marker:
        return 0

    if not args.github_token:
        print("::error::--github-token is required when --pr-body-marker or --pr-comment-marker is set", file=sys.stderr)
        return 1

    pr_num = resolve_pr_number(args.pr_number)
    if not pr_num:
        print(
            "::warning::No PR context detected (not triggered by a pull_request/issue event, "
            "and --pr-number not supplied) — skipping PR destinations",
            file=sys.stderr,
        )
        return 0

    repo = os.environ.get("GITHUB_REPOSITORY") or ""
    api = GitAPI(token=args.github_token, repo=repo, pr_number=pr_num)

    if args.pr_body_marker:
        body = api.get_pr_body()
        new_body = splice_pr_body(body, snippet, args.pr_body_marker)
        if new_body != body:
            api.update_pr_body(new_body)
            print(f"pr-body: updated PR #{pr_num} section <{args.pr_body_marker}>", file=sys.stderr)
        else:
            print(f"pr-body: no change to PR #{pr_num} section <{args.pr_body_marker}>", file=sys.stderr)

    if args.pr_comment_marker:
        api.upsert_sticky_comment(args.pr_comment_marker, snippet)
        print(f"pr-comment: upserted sticky comment <{args.pr_comment_marker}> on PR #{pr_num}", file=sys.stderr)

    return 0


def do_fetch_body(args: argparse.Namespace) -> int:
    if not args.github_token:
        print("::error::--github-token is required for --fetch-body", file=sys.stderr)
        return 1
    pr_num = resolve_pr_number(args.pr_number)
    if not pr_num:
        print("::warning::No PR context detected — emitting empty body", file=sys.stderr)
        write_output("", args.output)
        return 0
    repo = os.environ.get("GITHUB_REPOSITORY") or ""
    api = GitAPI(token=args.github_token, repo=repo, pr_number=pr_num)
    body = api.get_pr_body()
    write_output(body, args.output)
    return 0


def strip_marker_prefix(body: str, marker: str) -> str:
    """Remove the leading `<!-- <marker> -->\\n` tag from a sticky-comment
    body so the emitted content is ready to feed into --previous-body-file.

    Safe no-op when the body does not start with the tag (e.g. a comment
    authored by hand that doesn't follow the sticky convention)."""
    tag = f"<!-- {marker} -->\n"
    if body.startswith(tag):
        return body[len(tag):]
    return body


def do_fetch_comment(args: argparse.Namespace) -> int:
    if not args.github_token:
        print("::error::--github-token is required for --fetch-comment", file=sys.stderr)
        return 1
    if not args.marker:
        print("::error::--marker is required for --fetch-comment", file=sys.stderr)
        return 1
    pr_num = resolve_pr_number(args.pr_number)
    if not pr_num:
        print("::warning::No PR context detected — emitting empty body", file=sys.stderr)
        write_output("", args.output)
        return 0
    repo = os.environ.get("GITHUB_REPOSITORY") or ""
    api = GitAPI(token=args.github_token, repo=repo, pr_number=pr_num)
    existing = api.find_sticky_comment(args.marker)
    if not existing:
        write_output("", args.output)
        return 0
    body = (existing.get("body") or "").replace("\r", "")
    write_output(strip_marker_prefix(body, args.marker), args.output)
    return 0


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="gh_api.py",
        description="GitHub API helper for tfreport CI workflows.",
    )
    action = p.add_mutually_exclusive_group(required=True)
    action.add_argument("--send", action="store_true", help="push a rendered snippet to step-summary / PR body / sticky comment")
    action.add_argument("--fetch-body", action="store_true", help="emit current PR body text")
    action.add_argument("--fetch-comment", action="store_true", help="emit body of a sticky PR comment")

    p.add_argument("--github-token", default=os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN", ""))
    p.add_argument("--pr-number", default="", help="override PR number (auto-detected from GITHUB_EVENT_PATH by default)")
    p.add_argument("--output", default="-", help="output path (default: stdout) — used by --fetch-body and --fetch-comment")

    # --send inputs
    p.add_argument("--content-file", help="path to the rendered snippet (required for --send)")
    p.add_argument("--step-summary", action="store_true", help="append snippet to $GITHUB_STEP_SUMMARY")
    p.add_argument("--pr-body-marker", default="", help="splice snippet between <!-- BEGIN_<marker> --> / <!-- END_<marker> -->")
    p.add_argument("--pr-comment-marker", default="", help="upsert sticky comment identified by <!-- <marker> -->")

    # --fetch-comment input
    p.add_argument("--marker", default="", help="sticky-comment marker (required for --fetch-comment)")

    return p


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)

    if args.send:
        if not args.content_file:
            print("::error::--content-file is required with --send", file=sys.stderr)
            return 1
        return do_send(args)
    if args.fetch_body:
        return do_fetch_body(args)
    if args.fetch_comment:
        return do_fetch_comment(args)
    return 1


if __name__ == "__main__":
    sys.exit(main())
