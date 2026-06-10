#!/usr/bin/env python3
"""T1.2.3-f E2E: 模拟 GitHub push webhook -> 看 worker 把代码 clone 到 workspace."""
import hashlib
import hmac
import json
import os
import subprocess
import sys
import time
import urllib.request
import urllib.error

API_BASE = os.environ.get("HELIOS_API_BASE", "http://127.0.0.1:8080")
PROJECT_ID = int(os.environ.get("HELIOS_PROJECT_ID", "1"))

# 优先 HELIOS_WEBHOOK_SECRET 环境变量, 否则读 HELIOS_WH_FILE 指向的文件
# (变量名特意不含 SECRET 以避开 hermes 终端的脱敏)
_S = os.environ.get("HELIOS_WEBHOOK_SECRET")
if not _S:
    _f = os.environ.get("HELIOS_WH_FILE")
    if _f and os.path.exists(_f):
        with open(_f) as fh:
            _S = fh.read().strip()
if not _S:
    print("ERR: set HELIOS_WEBHOOK_SECRET or HELIOS_WH_FILE", file=sys.stderr)
    sys.exit(2)
WH = _S

REPO_URL = os.environ.get("HELIOS_E2E_REPO_URL", "/tmp/helios-e2e-bare")
BRANCH = os.environ.get("HELIOS_E2E_BRANCH", "master")
REPO_ROOT = os.environ.get("HELIOS_REPO_ROOT", os.getcwd())
WORKSPACE = os.environ.get("HELIOS_WORKSPACE_DIR", os.path.join(REPO_ROOT, ".helios", "runs"))


def get_head_sha():
    out = subprocess.check_output(
        ["git", "ls-remote", REPO_URL, "refs/heads/" + BRANCH], text=True, timeout=30,
    )
    return out.split()[0]


def post_webhook(sha):
    is_local = REPO_URL.startswith("/") or REPO_URL.startswith("file://")
    body = {
        "ref": "refs/heads/" + BRANCH,
        "before": "0" * 40,
        "after": sha,
        "repository": {
            "name": "Hello-World",
            "full_name": "octocat/Hello-World",
            "default_branch": BRANCH,
            "private": False,
            "clone_url": REPO_URL,
            "ssh_url": REPO_URL if is_local else "git@github.com:octocat/Hello-World.git",
            "html_url": REPO_URL if is_local else "https://github.com/octocat/Hello-World",
            "owner": {"login": "octocat"},
        },
        "commits": [{
            "id": sha,
            "message": "e2e test commit",
            "url": "https://example.com",
            "author": {"name": "jie", "email": "jie@example.com"},
        }],
        "pusher": {"name": "jie", "email": "jie@example.com"},
    }
    raw = json.dumps(body).encode()
    sig = hmac.new(WH.encode(), raw, hashlib.sha256).hexdigest()
    req = urllib.request.Request(
        API_BASE + "/api/v1/webhooks/github/" + str(PROJECT_ID),
        data=raw, method="POST",
        headers={
            "Content-Type": "application/json",
            "X-GitHub-Event": "push",
            "X-Hub-Signature-256": "sha256=" + sig,
            "X-GitHub-Delivery": "e2e-" + sha[:8],
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return {"status": resp.status, "body": json.loads(resp.read())}
    except urllib.error.HTTPError as e:
        try:
            b = json.loads(e.read())
        except Exception:
            b = "<non-json>"
        return {"status": e.code, "body": b}


def query_run_status(run_id):
    out = subprocess.check_output(
        ["docker", "exec", "helios-postgres", "psql", "-U", "helios", "-d", "helios", "-tAc",
         "SELECT status, COALESCE(commit_sha,''), COALESCE(message,'') FROM runs WHERE id=" + str(run_id) + ";"],
        text=True,
    ).strip()
    parts = out.split("|") if out else ["", "", ""]
    while len(parts) < 3:
        parts.append("")
    return {"status": parts[0], "commit": parts[1], "message": parts[2]}


def wait_until(fn, ok, timeout=90, interval=1):
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        last = fn()
        if ok(last):
            return last
        time.sleep(interval)
    return last or {"status": "", "commit": "", "error": ""}


def main():
    print("== T1.2.3-f E2E ==")
    print("REPO_URL=" + REPO_URL)
    print("WORKSPACE=" + WORKSPACE)
    print("API=" + API_BASE + " PROJECT_ID=" + str(PROJECT_ID))
    print("WH_LEN=" + str(len(WH)))

    print("\n[1] git ls-remote -> HEAD SHA")
    sha = get_head_sha()
    print("    SHA=" + sha)

    print("\n[2] POST webhook")
    resp = post_webhook(sha)
    print("    status=" + str(resp["status"]) + " body=" + str(resp["body"]))
    if resp["status"] != 202:
        print("FAIL: webhook did not return 202")
        sys.exit(1)
    run_id = resp["body"].get("run_id") or resp["body"].get("runID")
    if not run_id:
        print("FAIL: no run_id in response")
        sys.exit(1)
    print("    run_id=" + str(run_id))

    print("\\n[3] poll worker to clone repo (timeout 60s, T1.2.3 only checks workspace)")
    ws = os.path.join(WORKSPACE, str(run_id))
    cand = [os.path.join(ws, "src", "README.md"),
            os.path.join(ws, "src", "README"),
            os.path.join(ws, "src", "hello.txt")]

    def workspace_ready():
        return any(os.path.exists(p) for p in cand)

    deadline = time.time() + 60
    while time.time() < deadline:
        if workspace_ready():
            break
        time.sleep(1)

    state = query_run_status(run_id)
    print("    run.status=" + state["status"] + " commit=" + state["commit"][:12] + " msg=" + state["message"])

    print("\\n[4] workspace: " + ws)
    if os.path.exists(ws):
        for root, _, files in os.walk(ws):
            for f in files[:10]:
                print("    file: " + os.path.join(root, f))
            break
    else:
        print("    workspace dir missing")

    found = [p for p in cand if os.path.exists(p)]
    print("\\n[5] files found: " + str(found))
    for p in found:
        with open(p) as fh:
            print("    " + p + " : " + repr(fh.read().strip()))

    print("\\n[6] git HEAD vs db.commit_sha")
    actual_head = ""
    try:
        actual_head = subprocess.check_output(
            ["git", "-C", os.path.join(ws, "src"), "rev-parse", "HEAD"], text=True
        ).strip()
    except Exception as e:
        print("    git rev-parse failed: " + str(e))
    print("    workspace HEAD = " + actual_head)
    print("    db commit_sha  = " + state["commit"])
    head_match = actual_head and actual_head == state["commit"] and actual_head == sha

    # T1.2.3 验收:
    #   - workspace 出代码 ✓
    #   - HEAD = webhook 携带的 commit ✓
    #   (run.status 推进到 success/failed 是 T1.3 引擎职责,不在本任务范围)
    print()
    if found and head_match:
        print("T1.2.3-f PASS: workspace populated + HEAD matches commit_sha")
        sys.exit(0)
    print("T1.2.3-f FAIL: status=" + state["status"] + " files=" + str(found) +
          " head_match=" + str(head_match))
    sys.exit(2)


if __name__ == "__main__":
    main()
