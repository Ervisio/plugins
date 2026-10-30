import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

class AutoMergeStatusTest(unittest.TestCase):
    def run_case(self, outcome="success", policy=True):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root / "scripts").mkdir()
            (root / "bin").mkdir()
            shutil.copy(Path(__file__).parents[1] / "scripts/auto-merge.sh", root / "scripts/auto-merge.sh")
            (root / "scripts/review-policy.py").write_text("import sys; sys.exit(" + ("0" if policy else "1") + ")")
            (root / "bin/git").write_text("#!/bin/sh\nexit 0\n")
            gh = """#!/usr/bin/env python3
import json, os, pathlib, sys
args=sys.argv[1:]
with pathlib.Path("calls.jsonl").open("a") as f: f.write(json.dumps(args)+"\\n")
sha="a"*40
if args[0]=="workflow": pass
elif args[:2]==["pr","view"]: print("CLEAN")
elif args[0]=="api":
    endpoint=next(x for x in args if x.startswith("repos/"))
    if endpoint.endswith("/commits/"+sha+"/pulls"): print("31")
    elif endpoint.endswith("/pulls/31/merge"): print("true")
    elif "/statuses/" in endpoint: pass
    elif endpoint.endswith("/pulls/31"):
        print(json.dumps({"state":"open","user":{"login":"github-actions[bot]"},"head":{"sha":sha,"ref":"sync/database-1.2.2","repo":{"full_name":"Ervisio/plugins"}},"base":{"ref":"main"}}))
    elif endpoint.endswith("/git/ref/heads/main"): print("b"*40)
    elif endpoint.endswith("/check-runs"):
        print("check\\tcompleted\\t"+os.environ["OUTCOME"]+"\\t15368\\thttps://github.com/Ervisio/plugins/actions/runs/1")
        print("regtool (gofmt, go vet, go test)\\tcompleted\\tsuccess\\t15368\\thttps://github.com/Ervisio/plugins/actions/runs/2")
    else: raise SystemExit("unexpected endpoint: "+endpoint)
else: raise SystemExit("unexpected arguments")
"""
            (root / "bin/gh").write_text(gh)
            for p in (root / "bin").iterdir(): p.chmod(0o755)
            env=dict(os.environ, GH_TOKEN="test", GITHUB_REPOSITORY="Ervisio/plugins",
                     HEAD_SHA="a"*40, OUTCOME=outcome, PATH=str(root / "bin")+os.pathsep+os.environ["PATH"])
            p=subprocess.run(["bash","scripts/auto-merge.sh"],cwd=root,env=env,capture_output=True,text=True,timeout=10)
            calls=[json.loads(x) for x in (root / "calls.jsonl").read_text().splitlines()]
            return p,calls

    def test_reports_only_verified_statuses_then_merges(self):
        p,calls=self.run_case()
        self.assertEqual(p.returncode,0,p.stderr)
        posts=[c for c in calls if "POST" in c]
        self.assertEqual(len(posts),2)
        self.assertTrue(all("state=success" in c for c in posts))
        self.assertTrue(all(any(x.endswith("/statuses/"+"a"*40) for x in c) for c in posts))
        self.assertTrue(any("PUT" in c for c in calls))

    def test_failed_validation_never_reports_success_or_merges(self):
        p,calls=self.run_case(outcome="failure")
        self.assertEqual(p.returncode,0,p.stderr)
        self.assertFalse(any("POST" in c or "PUT" in c for c in calls))

    def test_policy_refusal_never_reports_success_or_merges(self):
        p,calls=self.run_case(policy=False)
        self.assertEqual(p.returncode,0,p.stderr)
        self.assertFalse(any("POST" in c or "PUT" in c for c in calls))
