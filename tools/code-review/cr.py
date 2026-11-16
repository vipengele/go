#!/usr/bin/env python3
"""Checkpoint store of the repository code review tracked in the control issue.

Every step of the review writes its result under this directory and pushes it,
so the files on the checkpoint branch are the whole state of the review:

  findings/<unit>.json        what a unit's reviewer reported, unverified
  verdicts/<unit>/<n>.json    the verifier's verdict on finding n (1-based)
  filed/<unit>/<n>.json       the issue filed for finding n, or why none was
  done/<unit>.json            the unit is reviewed, verified, filed and recorded
  extras.json                 the units the completeness pass added

A unit id is <area>:<lens>; in a file name the colon is written "--".

Commands:
  status                        print the state as JSON
  save <path>                   validate a findings, verdict or extras file, commit and push it
  drafts <unit>                 print the issues still to file for a unit, as JSON
  filed <unit> <n> created <issue-number>
  filed <unit> <n> skipped <existing-issue-or-0> <reason...>
  comment <unit>                print the progress comment for a unit, as markdown
  done <unit>                   mark a unit done; fails while a step is missing
  report                        print the final summary, as markdown
"""
import fcntl
import json
import os
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
BRANCH = "ccr-af36d1cd-8vhwcf"
CONTROL = 203
COMMIT = "e83a35a"

SEVERITIES = ["critical", "high", "medium", "low"]
CATEGORIES = ["bug", "security", "performance", "docs-drift", "feature-gap", "test-quality", "ci"]
PROJECTS = ["cli", "update", "daemon", "config", "commons", "filelock", "repo"]
TYPES = ["Bug", "Feature", "Task"]
VERDICTS = ["confirmed", "refuted", "uncertain"]
CATEGORY_LABEL = {"security": "security", "performance": "performance", "docs-drift": "docs",
                  "test-quality": "testing", "ci": "ci-cd"}


def die(msg):
    sys.exit("cr.py: " + msg)


def fname(unit):
    return unit.replace(":", "--")


def load(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def dump(path, value):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(value, f, indent=1, ensure_ascii=False)
        f.write("\n")


def git(*args, check=True):
    return subprocess.run(["git", "-C", ROOT, *args], check=check, capture_output=True, text=True)


def commit(path, what):
    """Commits one path and pushes. Serialised, since review agents run side by side."""
    with open("/tmp/code-review.lock", "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if git("rev-parse", "--abbrev-ref", "HEAD").stdout.strip() != BRANCH:
            die(f"the checkout is not on branch {BRANCH}")
        if "noreply@anthropic.com" in git("var", "GIT_COMMITTER_IDENT").stdout:
            die("git identity is not set; run `agtk cloud init` first")
        git("add", "--", path)
        if git("diff", "--cached", "--quiet", check=False).returncode != 0:
            git("commit", "-q", "-m", f"chore(code-review): checkpoint {what}")
        err = ""
        for attempt in range(4):
            push = git("push", "-q", "origin", f"HEAD:{BRANCH}", check=False)
            if push.returncode == 0:
                return
            err = push.stderr.strip()
            time.sleep(2 * 2 ** attempt)
        die("push failed, the checkpoint is committed locally only: " + err)


def findings(unit):
    path = os.path.join(HERE, "findings", fname(unit) + ".json")
    return load(path)["findings"] if os.path.exists(path) else None


def numbered(kind, unit):
    """The records of one kind for a unit, keyed by finding number."""
    d = os.path.join(HERE, kind, fname(unit))
    out = {}
    if os.path.isdir(d):
        for name in os.listdir(d):
            if name.endswith(".json"):
                out[int(name[:-5])] = load(os.path.join(d, name))
    return out


def is_done(unit):
    return os.path.exists(os.path.join(HERE, "done", fname(unit) + ".json"))


def need(value, keys, where):
    for k in keys:
        if not isinstance(value.get(k), str) or not value[k].strip():
            die(f"{where}: missing or empty \"{k}\"")


def one_of(value, key, allowed, where):
    if value.get(key) not in allowed:
        die(f"{where}: \"{key}\" must be one of {allowed}")


def cmd_status():
    units = []
    d = os.path.join(HERE, "findings")
    for name in sorted(os.listdir(d)) if os.path.isdir(d) else []:
        unit = name[:-5].replace("--", ":")
        units.append({"id": unit, "n": len(findings(unit)),
                      "verified": sorted(numbered("verdicts", unit)), "done": is_done(unit)})
    extras = os.path.join(HERE, "extras.json")
    print(json.dumps({"units": units,
                      "extras": load(extras)["units"] if os.path.exists(extras) else None,
                      "completeness_done": is_done("completeness")}))


def cmd_save(path):
    path = os.path.abspath(path)
    rel = os.path.relpath(path, HERE)
    try:
        value = load(path)
    except (OSError, ValueError) as e:
        die(f"{rel}: not readable JSON: {e}")
    parts = rel.split(os.sep)
    if parts[0] == "findings" and len(parts) == 2:
        if value.get("unit") != parts[1][:-5].replace("--", ":"):
            die(f"{rel}: \"unit\" does not match the file name")
        if not isinstance(value.get("findings"), list):
            die(f"{rel}: \"findings\" must be a list")
        for i, f in enumerate(value["findings"], 1):
            where = f"{rel} finding {i}"
            need(f, ["title", "description", "evidence"], where)
            one_of(f, "category", CATEGORIES, where)
            one_of(f, "severity", SEVERITIES, where)
            if not isinstance(f.get("locations"), list) or not f["locations"]:
                die(f"{where}: \"locations\" must be a non-empty list of path:line")
    elif parts[0] == "verdicts" and len(parts) == 3:
        unit = parts[1].replace("--", ":")
        n = int(parts[2][:-5])
        fs = findings(unit)
        if fs is None or not 1 <= n <= len(fs):
            die(f"{rel}: no finding {n} in unit {unit}")
        need(value, ["reason"], rel)
        one_of(value, "verdict", VERDICTS, rel)
        if value["verdict"] == "confirmed":
            need(value, ["title", "body"], rel)
            one_of(value, "severity", SEVERITIES, rel)
            one_of(value, "category", CATEGORIES, rel)
            one_of(value, "project", PROJECTS, rel)
            one_of(value, "issue_type", TYPES, rel)
            for section in ["## Summary", "## Where", "## Evidence", "## Impact", "## Suggested direction"]:
                if section not in value["body"]:
                    die(f"{rel}: body has no \"{section}\" section")
    elif rel == "extras.json":
        if not isinstance(value.get("units"), list):
            die("extras.json: \"units\" must be a list")
        for u in value["units"]:
            need(u, ["id", "paths", "focus", "project"], "extras.json")
            if not u["id"].startswith("extra-") or ":" in u["id"]:
                die("extras.json: every id starts with \"extra-\" and has no colon")
    else:
        die(f"{rel}: not a findings, verdict or extras file")
    commit(path, rel)
    print("saved " + rel)


def drafts(unit):
    fs = findings(unit) or []
    verdicts, filed = numbered("verdicts", unit), numbered("filed", unit)
    out = []
    for n in range(1, len(fs) + 1):
        v = verdicts.get(n)
        if not v or v["verdict"] != "confirmed" or n in filed:
            continue
        labels = ["code-review", "severity:" + v["severity"], "project:" + v["project"]]
        if v["category"] in CATEGORY_LABEL:
            labels.append(CATEGORY_LABEL[v["category"]])
        if v.get("windows"):
            labels.append("windows")
        body = (v["body"].rstrip() + "\n\n---\nPart of the repository code review tracked in "
                f"#{CONTROL} (unit `{unit}`, commit `{COMMIT}`).")
        out.append({"n": n, "title": v["title"], "type": v["issue_type"], "labels": labels, "body": body})
    return out


def cmd_filed(unit, n, kind, *rest):
    n = int(n)
    if n not in numbered("verdicts", unit):
        die(f"finding {n} of {unit} has no verdict")
    if kind == "created" and len(rest) == 1:
        record = {"created": int(rest[0])}
    elif kind == "skipped" and len(rest) >= 2:
        record = {"skipped": " ".join(rest[1:]), "existing": int(rest[0])}
    else:
        die("usage: filed <unit> <n> created <number> | skipped <existing-or-0> <reason...>")
    path = os.path.join(HERE, "filed", fname(unit), f"{n}.json")
    dump(path, record)
    commit(path, os.path.relpath(path, HERE))
    print("recorded")


def cmd_comment(unit):
    fs = findings(unit) or []
    verdicts, filed = numbered("verdicts", unit), numbered("filed", unit)
    created, skipped, rejected = [], [], []
    for n in range(1, len(fs) + 1):
        v, f = verdicts.get(n), filed.get(n)
        if v and v["verdict"] == "confirmed" and f:
            if "created" in f:
                created.append(f"- #{f['created']} {v['title']}")
            else:
                ref = f" — #{f['existing']}" if f.get("existing") else ""
                skipped.append(f"- {v['title']} — {f['skipped']}{ref}")
        elif v:
            rejected.append(f"- {fs[n - 1]['title']} — **{v['verdict']}**: {v['reason']}")
    def block(lines):
        return "\n".join(lines) if lines else "none"
    print(f"### Unit `{unit}` done\n\n{len(fs)} reported, {len(created)} filed, {len(skipped)} skipped, "
          f"{len(rejected)} not confirmed.\n\n**Filed**\n{block(created)}\n\n**Skipped**\n{block(skipped)}"
          f"\n\n**Not filed after verification**\n{block(rejected)}")


def cmd_done(unit):
    if unit != "completeness":
        fs = findings(unit)
        if fs is None:
            die(f"unit {unit} has no findings file")
        verdicts, filed = numbered("verdicts", unit), numbered("filed", unit)
        for n in range(1, len(fs) + 1):
            if n not in verdicts:
                die(f"finding {n} of {unit} has no verdict")
            if verdicts[n]["verdict"] == "confirmed" and n not in filed:
                die(f"finding {n} of {unit} is confirmed and not filed")
    path = os.path.join(HERE, "done", fname(unit) + ".json")
    dump(path, {"unit": unit})
    commit(path, os.path.relpath(path, HERE))
    print("done " + unit)


def cmd_report():
    rows = []
    d = os.path.join(HERE, "findings")
    for name in sorted(os.listdir(d)):
        unit = name[:-5].replace("--", ":")
        verdicts, filed = numbered("verdicts", unit), numbered("filed", unit)
        for n, f in filed.items():
            if "created" in f:
                rows.append((verdicts[n], f["created"]))
    by_sev = {s: sum(1 for v, _ in rows if v["severity"] == s) for s in SEVERITIES}
    by_cat = {c: sum(1 for v, _ in rows if v["category"] == c) for c in CATEGORIES}
    out = [f"## Review complete\n\n{len(rows)} issues filed.\n", "| Severity | Issues |", "|---|---|"]
    out += [f"| {s} | {by_sev[s]} |" for s in SEVERITIES]
    out += ["", "| Category | Issues |", "|---|---|"]
    out += [f"| {c} | {by_cat[c]} |" for c in CATEGORIES]
    for p in PROJECTS:
        mine = sorted(((v, num) for v, num in rows if v["project"] == p),
                      key=lambda r: (SEVERITIES.index(r[0]["severity"]), r[1]))
        if mine:
            out.append(f"\n### {p}\n")
            out += [f"- #{num} ({v['severity']}) {v['title']}" for v, num in mine]
    print("\n".join(out))


def main():
    args = sys.argv[1:]
    if not args:
        die(__doc__)
    cmd, rest = args[0], args[1:]
    if cmd == "status":
        cmd_status()
    elif cmd == "save" and len(rest) == 1:
        cmd_save(rest[0])
    elif cmd == "drafts" and len(rest) == 1:
        print(json.dumps(drafts(rest[0]), indent=1, ensure_ascii=False))
    elif cmd == "filed" and len(rest) >= 4:
        cmd_filed(*rest)
    elif cmd == "comment" and len(rest) == 1:
        cmd_comment(rest[0])
    elif cmd == "done" and len(rest) == 1:
        cmd_done(rest[0])
    elif cmd == "report":
        cmd_report()
    else:
        die(__doc__)


if __name__ == "__main__":
    main()
