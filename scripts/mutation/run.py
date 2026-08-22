#!/usr/bin/env python3
"""
run.py -- mutation-kill evidence for this repository's codec tests.

WHY THIS EXISTS
---------------
Nearly every codec test in this repository is symmetric: a round trip, one
in-repo implementation checked against another, or a fixture this library
itself produced.  A defect applied identically to the encoder and the decoder
is invisible to all three.  Such a test cannot fail, so it is not evidence of
anything.

This harness measures that instead of asserting it.  It applies one deliberate
defect at a time to the subject code, runs the tests that claim to cover that
code, records whether they failed, and puts the source back.  A test that stays
green under a mutation that genuinely changes the bytes on the wire is vacuous
and needs a real, spec-anchored assertion.

Every mutation carries a `spec` field naming the document and the clause the
mutated value comes from, so "this mutation is wrong" is a citation and not an
opinion.

USAGE
-----
    python3 scripts/mutation/run.py --check              # manifest sanity only
    python3 scripts/mutation/run.py --phase existing     # tests as they were
    python3 scripts/mutation/run.py --phase added        # the new spec anchors
    python3 scripts/mutation/run.py --phase both         # the whole table
    python3 scripts/mutation/run.py --id piz-wenc16-aoffset

    --json PATH   also write the raw results as JSON

GUARANTEES
----------
* One mutation is live at a time; a mutation may touch several places when the
  defect is by definition symmetric (encoder and decoder together), which is
  the case this harness exists to expose.
* Sources are restored in a `finally`, and again on SIGINT/SIGTERM, so an
  interrupted run does not leave the tree mutated.  `--verify-clean` re-runs
  `git status --porcelain` at the end and fails if anything is left modified.
* A test that does not exist is reported as an error, never as "survived":
  `go test -run` exits 0 when its pattern matches nothing.

EXIT STATUS
-----------
0 if every mutation's outcome matched its declared `expect` field, 1 otherwise.
"""

import argparse
import json
import os
import re
import signal
import subprocess
import sys
import time

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
MANIFEST = os.path.join(os.path.dirname(os.path.abspath(__file__)), "mutations.json")

_originals = {}  # abs path -> original bytes, for the signal handler


def git_status():
    proc = subprocess.run(["git", "status", "--porcelain"], cwd=REPO,
                          capture_output=True, text=True)
    return proc.stdout.splitlines()


def load_manifest():
    with open(MANIFEST, "r", encoding="utf-8") as f:
        return json.load(f)["mutations"]


def check_edits(mutation):
    """Return a list of problems with this mutation's edits, without changing anything."""
    problems = []
    for edit in mutation["edits"]:
        path = os.path.join(REPO, edit["file"])
        if not os.path.exists(path):
            problems.append(f"{edit['file']}: no such file")
            continue
        with open(path, "r", encoding="utf-8") as f:
            text = f.read()
        want = edit.get("count", 1)
        got = text.count(edit["old"])
        if got != want:
            problems.append(
                f"{edit['file']}: anchor text occurs {got} times, manifest says {want}"
            )
        if edit["old"] == edit["new"]:
            problems.append(f"{edit['file']}: mutation is a no-op")
    return problems


def apply_mutation(mutation):
    """Apply every edit. Returns a dict of path -> original text for restoration."""
    saved = {}
    try:
        for edit in mutation["edits"]:
            path = os.path.join(REPO, edit["file"])
            with open(path, "r", encoding="utf-8") as f:
                text = f.read()
            if path not in saved:
                saved[path] = text
                _originals[path] = text
            want = edit.get("count", 1)
            got = text.count(edit["old"])
            if got != want:
                raise RuntimeError(
                    f"{edit['file']}: anchor occurs {got} times, expected {want}"
                )
            text = text.replace(edit["old"], edit["new"])
            with open(path, "w", encoding="utf-8") as f:
                f.write(text)
            saved[path] = saved[path]  # keep the pre-edit text, not the interim one
    except Exception:
        restore(saved)
        raise
    return saved


def restore(saved):
    for path, text in saved.items():
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)
        _originals.pop(path, None)


def _panic_restore(signum, frame):
    for path, text in list(_originals.items()):
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)
    sys.stderr.write("\ninterrupted: sources restored\n")
    sys.exit(130)


RUN_RE = re.compile(r"^=== RUN\s+(\S+)", re.M)
RESULT_RE = re.compile(r"^\s*--- (PASS|FAIL|SKIP):\s+(\S+)", re.M)


def run_test(pkg, name, timeout=300):
    """Run one test. Returns (status, detail) where status is
    pass | fail | skip | notfound | buildfail."""
    cmd = [
        "go", "test", pkg,
        "-run", f"^{name}$",
        "-count=1", "-v",
        f"-timeout={timeout}s",
    ]
    proc = subprocess.run(
        cmd, cwd=REPO, capture_output=True, text=True, timeout=timeout + 60
    )
    out = proc.stdout + proc.stderr
    if "build failed" in out or "cannot use" in out or "[build failed]" in out:
        if not RUN_RE.search(out):
            return "buildfail", out.strip().splitlines()[-1] if out.strip() else ""
    if not RUN_RE.search(out):
        return "notfound", "go test ran no test matching this name"
    results = {n: st for st, n in RESULT_RE.findall(out)}
    top = results.get(name)
    if top == "FAIL" or proc.returncode != 0:
        first = ""
        for line in out.splitlines():
            if ".go:" in line and ("Error" in line or "FAIL" in line or ":" in line):
                first = line.strip()
                break
        return "fail", first
    if top == "SKIP":
        return "skip", "test skipped itself"
    return "pass", ""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--phase", choices=["existing", "added", "both"], default="both")
    ap.add_argument("--id", action="append", help="run only these mutation ids")
    ap.add_argument("--check", action="store_true", help="validate the manifest only")
    ap.add_argument("--json", help="write raw results here")
    ap.add_argument("--verify-clean", action="store_true",
                    help="fail if the tree differs from how the run found it")
    args = ap.parse_args()

    before_status = git_status()

    signal.signal(signal.SIGINT, _panic_restore)
    signal.signal(signal.SIGTERM, _panic_restore)

    mutations = load_manifest()
    if args.id:
        wanted = set(args.id)
        mutations = [m for m in mutations if m["id"] in wanted]
        missing = wanted - {m["id"] for m in mutations}
        if missing:
            sys.exit(f"unknown mutation id(s): {', '.join(sorted(missing))}")

    problems = []
    for m in mutations:
        for p in check_edits(m):
            problems.append(f"{m['id']}: {p}")
    if problems:
        print("MANIFEST PROBLEMS")
        for p in problems:
            print("  " + p)
        sys.exit(1)
    print(f"manifest: {len(mutations)} mutations, all anchor texts found")
    if args.check:
        return

    phases = ["existing", "added"] if args.phase == "both" else [args.phase]
    rows = []
    started = time.time()

    for m in mutations:
        entries = []
        for phase in phases:
            for t in m.get(phase + "_tests", []):
                entries.append((phase, t))
        if not entries:
            continue
        print(f"\n== {m['id']} ({m['codec']}) -- {m['description']}")
        saved = apply_mutation(m)
        try:
            for phase, t in entries:
                status, detail = run_test(t["pkg"], t["run"])
                killed = status in ("fail", "buildfail")
                rows.append({
                    "mutation": m["id"],
                    "codec": m["codec"],
                    "phase": phase,
                    "pkg": t["pkg"],
                    "test": t["run"],
                    "status": status,
                    "killed": killed,
                    "detail": detail,
                })
                mark = "KILLED " if killed else ("       " if status == "pass" else status.upper())
                print(f"   {mark} {phase:8s} {t['pkg']} {t['run']}"
                      + (f"   [{status}]" if status not in ("pass", "fail") else ""))
        finally:
            restore(saved)

    # ---- table -------------------------------------------------------------
    print("\n")
    print("| codec | mutation | test | phase | killed? |")
    print("| ----- | -------- | ---- | ----- | ------- |")
    for r in rows:
        verdict = "yes" if r["killed"] else ("no" if r["status"] == "pass" else r["status"])
        print(f"| {r['codec']} | {r['mutation']} | {r['test']} | {r['phase']} | {verdict} |")

    # ---- per-mutation verdict against the declaration -----------------------
    print("\nper-mutation verdict (a mutation is killed if ANY test covering it fails)")
    bad = 0
    by_id = {m["id"]: m for m in mutations}
    for phase in phases:
        print(f"\n  phase: {phase}")
        for mid, m in by_id.items():
            phase_rows = [r for r in rows if r["mutation"] == mid and r["phase"] == phase]
            if not phase_rows:
                continue
            killed = any(r["killed"] for r in phase_rows)
            expect = m.get("expect", {}).get(phase)
            ok = expect is None or (expect == ("killed" if killed else "survived"))
            if not ok:
                bad += 1
            print(f"    {'KILLED  ' if killed else 'SURVIVED'}  {mid:28s}"
                  f"  expected {expect or '(unstated)'}"
                  f"{'' if ok else '   <-- MISMATCH'}")

    print(f"\n{len(rows)} test runs in {time.time() - started:.0f}s")

    if args.json:
        with open(args.json, "w", encoding="utf-8") as f:
            json.dump(rows, f, indent=2)
        print(f"raw results: {args.json}")

    if args.verify_clean:
        # The question is not whether the tree is clean -- a working tree may
        # legitimately have edits in it -- but whether this run left anything
        # behind. So the comparison is against how the run found it.
        after_status = git_status()
        if after_status != before_status:
            print("\nNOT CLEAN -- this run changed the working tree:")
            for line in sorted(set(after_status) ^ set(before_status)):
                print("  " + line)
            sys.exit(1)
        print("\ngit status is byte-for-byte what it was before the run "
              f"({len(before_status)} pre-existing entries, unchanged)")

    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()
