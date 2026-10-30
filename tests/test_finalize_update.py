import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

class FinalizeUpdateTest(unittest.TestCase):
    def run_case(self, outcome="success", policy=True):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root / "scripts").mkdir()
            (root / "bin").mkdir()
            shutil.copy(Path(__file__).parents[1] / "scripts/finalize-update.sh", root / "scripts/finalize-update.sh")
            (root / "scripts/review-policy.py").write_text("import sys; sys.exit(" + ("0" if policy else "1") + ")")
            (root / "scripts/auto-merge.sh").write_text('test "$HEAD_SHA" = "' + "a" * 40 + '"\ntouch merged\n')
            (root / "bin/git").write_text("#!/bin/sh\nexit 0\n")
            gh = """#!/usr/bin/env python3
import os, pathlib, sys
args=sys.argv[1:]
if args[:2]==["pr","view"]:
    if "number" in args:
        print("31")
    else:
        print(("MERGED" if pathlib.Path("merged").exists() else "OPEN")+"\\t"+"a"*40)
elif args[0]=="api":
    print("check\\tcompleted\\t"+os.environ["OUTCOME"]+"\\t15368")
    print("regtool (gofmt, go vet, go test)\\tcompleted\\tsuccess\\t15368")
else:
    raise SystemExit("unexpected command: "+str(args))
"""
            (root / "bin/gh").write_text(gh)
            for p in (root / "bin").iterdir():
                p.chmod(0o755)
            env = dict(os.environ, GH_TOKEN="test", REGISTRY_REPO="Ervisio/plugins",
                       PATH=str(root / "bin") + os.pathsep + os.environ["PATH"], OUTCOME=outcome)
            p = subprocess.run(["bash", "scripts/finalize-update.sh", "sync/database-1.2.2"],
                               cwd=root, env=env, capture_output=True, text=True, timeout=10)
            return p, (root / "merged").exists()

    def test_successful_checks_finalize_exact_head(self):
        p, merged = self.run_case()
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertTrue(merged)

    def test_failed_check_never_merges(self):
        p, merged = self.run_case(outcome="failure")
        self.assertNotEqual(p.returncode, 0)
        self.assertFalse(merged)
        self.assertIn("will not be published", p.stdout)

    def test_permission_changes_do_not_auto_merge(self):
        p, merged = self.run_case(policy=False)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertFalse(merged)
        self.assertIn("maintainer review", p.stdout)

if __name__ == "__main__":
    unittest.main()
