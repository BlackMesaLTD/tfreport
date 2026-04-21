#!/usr/bin/env python3
"""Send a rendered tfreport snippet to one or more GitHub destinations.

Invoked by tfreport-send/action.yml. Reads configuration from the
INPUT_* environment variables set by the action.

Destinations (all independent, any combination):
- Step summary: appends to $GITHUB_STEP_SUMMARY (no auth required).
- PR body:     splices between <!-- BEGIN_<marker> --> / <!-- END_<marker> -->
               in the PR description; if markers are absent, appends them
               at the end of the body.
- PR comment:  upserts a sticky comment identified by a <!-- <marker> --> tag
               at the start of the comment body; edits the matching comment
               if present, otherwise posts a new one.

PR destinations auto-detect the PR number from $GITHUB_EVENT_PATH unless
INPUT_PR_NUMBER overrides. If no PR context is present, PR destinations
are skipped with a warning (non-fatal).
"""
from __future__ import annotations

import json
import os
import re
import sys
import urllib.error
import urllib.request


def env(name: str, default: str = "") -> str:
    return (os.environ.get(name) or default).strip()


def load_snippet(path: str) -> str:
    if not os.path.isfile(path):
        print(f"::error::content-file not found: {path}")
        sys.exit(1)
    with open(path, encoding="utf-8") as f:
        return f.read().strip()


def resolve_pr_number(explicit: str) -> str | None:
    if explicit:
        return explicit
    event_path = env("GITHUB_EVENT_PATH")
    if not event_path or not os.path.isfile(event_path):
        return None
    with open(event_path, encoding="utf-8") as f:
        event = json.load(f)
    pr = event.get("pull_request") or {}
    issue = event.get("issue") or {}
    num = pr.get("number") or issue.get("number")
    return str(num) if num else None


def api(method: str, path: str, token: str, data: dict | None = None) -> dict | list | None:
    url = f"https://api.github.com{path}"
    req = urllib.request.Request(url, method=method)
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("Accept", "application/vnd.github+json")
    req.add_header("X-GitHub-Api-Version", "2022-11-28")
    if data is not None:
        req.add_header("Content-Type", "application/json")
        req.data = json.dumps(data).encode("utf-8")
    try:
        with urllib.request.urlopen(req) as r:
            raw = r.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", errors="replace")
        print(f"::error::GitHub API {method} {path} failed: {e.code} {detail}")
        sys.exit(1)


def deliver_step_summary(snippet: str) -> None:
    path = env("GITHUB_STEP_SUMMARY")
    if not path:
        print("::warning::step-summary requested but $GITHUB_STEP_SUMMARY is not set")
        return
    with open(path, "a", encoding="utf-8") as f:
        f.write("\n" + snippet + "\n")
    print("step-summary: appended")


def deliver_pr_body(snippet: str, marker: str, repo: str, pr_num: str, token: str) -> None:
    pr = api("GET", f"/repos/{repo}/pulls/{pr_num}", token) or {}
    body = (pr.get("body") or "").replace("\r", "")

    begin = f"<!-- BEGIN_{marker} -->"
    end = f"<!-- END_{marker} -->"
    new_section = f"{begin}\n{snippet}\n{end}"
    pattern = re.compile(re.escape(begin) + r".*?" + re.escape(end), re.DOTALL)

    if pattern.search(body):
        new_body = pattern.sub(new_section, body)
    elif body:
        new_body = body.rstrip() + "\n\n" + new_section + "\n"
    else:
        new_body = new_section + "\n"

    if new_body == body:
        print(f"pr-body: no change to PR #{pr_num} section <{marker}>")
        return
    api("PATCH", f"/repos/{repo}/pulls/{pr_num}", token, {"body": new_body})
    print(f"pr-body: updated PR #{pr_num} section <{marker}>")


def find_sticky_comment(repo: str, pr_num: str, tag: str, token: str) -> dict | None:
    page = 1
    while True:
        comments = api(
            "GET",
            f"/repos/{repo}/issues/{pr_num}/comments?per_page=100&page={page}",
            token,
        ) or []
        if not comments:
            return None
        for c in comments:
            if (c.get("body") or "").startswith(tag):
                return c
        if len(comments) < 100:
            return None
        page += 1


def deliver_pr_comment(snippet: str, marker: str, repo: str, pr_num: str, token: str) -> None:
    tag = f"<!-- {marker} -->"
    new_body = f"{tag}\n{snippet}\n"
    existing = find_sticky_comment(repo, pr_num, tag, token)
    if existing:
        if (existing.get("body") or "").replace("\r", "") == new_body:
            print(f"pr-comment: no change to comment {existing['id']} on PR #{pr_num}")
            return
        api(
            "PATCH",
            f"/repos/{repo}/issues/comments/{existing['id']}",
            token,
            {"body": new_body},
        )
        print(f"pr-comment: updated comment {existing['id']} on PR #{pr_num}")
    else:
        api(
            "POST",
            f"/repos/{repo}/issues/{pr_num}/comments",
            token,
            {"body": new_body},
        )
        print(f"pr-comment: created new comment on PR #{pr_num} with signature <{marker}>")


def main() -> int:
    snippet = load_snippet(env("INPUT_CONTENT_FILE"))

    step_summary = env("INPUT_STEP_SUMMARY").lower() == "true"
    body_marker = env("INPUT_PR_BODY_MARKER")
    comment_marker = env("INPUT_PR_COMMENT_MARKER")
    token = env("INPUT_GITHUB_TOKEN")
    repo = env("GITHUB_REPOSITORY")
    pr_num_override = env("INPUT_PR_NUMBER")

    if step_summary:
        deliver_step_summary(snippet)

    if not body_marker and not comment_marker:
        return 0

    if not token:
        print("::error::github-token is required when pr-body-marker or pr-comment-marker is set")
        return 1

    pr_num = resolve_pr_number(pr_num_override)
    if not pr_num:
        print(
            "::warning::No PR context detected "
            "(not triggered by a pull_request/issue event, and pr-number not supplied) "
            "— skipping PR destinations"
        )
        return 0

    if body_marker:
        deliver_pr_body(snippet, body_marker, repo, pr_num, token)

    if comment_marker:
        deliver_pr_comment(snippet, comment_marker, repo, pr_num, token)

    return 0


if __name__ == "__main__":
    sys.exit(main())
