#!/usr/bin/env python3
# SPDX-AI-Disclosure: ai-generated
# SPDX-AI-Model: GPT-6.0 Astra
# SPDX-AI-Provider: OpenAI
# SPDX-AI-Scope: Release workflow regression tests generated under maintainer direction.
# SPDX-AI-Date: 2026-10-29

"""Execute the production release script with isolated local command stubs.

Python's standard library, Bash and jq suffice; no YAML package, credentials or
network access is needed. The gh stub never delegates to the real GitHub CLI.
"""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("release.sh").resolve()
CANDIDATE = "a" * 40
OTHER = "b" * 40

# This fixture simulates remote state, not the gate's decision. CI finishes on
# the original SHA while a ref may change during a pending tag workflow. jq is
# real so the production script's JSON parsing is exercised as well.
GH_STUB = r'''#!/usr/bin/env python3
import json, os, subprocess, sys
from pathlib import Path

state_file = Path(os.environ["RELEASE_TEST_STATE"])
state = json.loads(state_file.read_text())
mode = state["mode"]
args = sys.argv[1:]
candidate = "a" * 40
other = "b" * 40
state["calls"].append(args)

def finish(response=None, error=None):
    state_file.write_text(json.dumps(state))
    if error:
        print(error, file=sys.stderr)
        sys.exit(1)
    if response is not None:
        if "--jq" in args:
            selector = args[args.index("--jq") + 1]
            result = subprocess.run(["jq", "-r", selector],
                                    input=json.dumps(response), text=True)
            sys.exit(result.returncode)
        print(json.dumps(response))
    sys.exit(0)

if args[:2] == ["release", "create"]:
    state["published"] = True
    state["published_main"] = state["main"]
    state["published_commit"] = state["tag_commit"]
    state["release_args"] = args
    state["notes"] = Path(args[args.index("--notes-file") + 1]).read_text()
    finish()

if not args or args[0] != "api":
    finish(error="Unexpected gh invocation")
endpoint = next((a for a in args if a.startswith("repos/fixture/repo/")), "")
if endpoint.endswith("/git/ref/heads/main"):
    state["main_reads"] += 1
    if state["main_reads"] > 1 and mode == "main_api_error":
        finish(error="Simulated final main API failure")
    finish({"object": {"type": "commit", "sha": state["main"]}})
elif endpoint.endswith("/actions/workflows/ci.yml/runs"):
    state["polls"] += 1
    if state["polls"] == 2:
        if mode == "main_moves":
            state["main"] = other
        if mode in ("tag_moves", "annotated_tag_moves"):
            state["tag_commit"] = other
    main_conclusion = "failure" if mode == "main_ci_failure" else "success"
    tag_conclusion = "failure" if mode == "tag_ci_failure" else "success"
    tag_status = "completed" if state["polls"] >= 3 else "in_progress"
    finish({"workflow_runs": [
        {"head_branch": "main", "head_sha": candidate, "status": "completed",
         "conclusion": main_conclusion, "run_number": 1,
         "html_url": "https://example.invalid/main-ci"},
        {"head_branch": "v1.1.0", "head_sha": candidate, "status": tag_status,
         "conclusion": tag_conclusion, "run_number": 2,
         "html_url": "https://example.invalid/tag-ci"}]})
elif endpoint.endswith("/git/ref/tags/v1.1.0"):
    if mode == "tag_deleted":
        finish(error="Simulated missing remote tag")
    if mode == "malformed_tag":
        finish({"object": {"type": "commit", "sha": None}})
    if mode == "non_commit_tag":
        finish({"object": {"type": "tree", "sha": "c" * 40}})
    if state["annotated"]:
        # Moving an annotated tag changes the ref to a different tag object.
        oid = "e" * 40 if mode == "annotated_tag_moves" else "c" * 40
        finish({"object": {"type": "tag", "sha": oid}})
    finish({"object": {"type": "commit", "sha": state["tag_commit"]}})
elif "/git/tags/" in endpoint:
    if mode == "tag_object_api_error":
        finish(error="Simulated annotated-tag API failure")
    if mode == "tag_cycle":
        finish({"object": {"type": "tag", "sha": "c" * 40}})
    if mode == "nested_annotated" and endpoint.endswith("c" * 40):
        finish({"object": {"type": "tag", "sha": "d" * 40}})
    finish({"object": {"type": "commit", "sha": state["tag_commit"]}})
else:
    finish(error="Unexpected API endpoint: " + endpoint)
'''


class ReleaseGateTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Missing prerequisites are errors, never a skipped release gate test.
        for command in ("bash", "jq", "python3"):
            if not shutil.which(command):
                raise RuntimeError("Missing test prerequisite: " + command)

    def run_release(self, mode):
        with tempfile.TemporaryDirectory(prefix="smppkit-release-test-") as tmp:
            root = Path(tmp)
            (root / "bin").mkdir()
            (root / "docs/releases").mkdir(parents=True)
            (root / "docs/releases/v1.1.0.md").write_text("Fixture notes.\n")
            state_file = root / "state.json"
            state_file.write_text(json.dumps({
                "mode": mode, "main": CANDIDATE, "tag_commit": CANDIDATE,
                "annotated": mode in ("annotated", "nested_annotated",
                                      "annotated_tag_moves", "tag_cycle",
                                      "tag_object_api_error"),
                "main_reads": 0, "polls": 0, "published": False, "calls": [],
            }))
            stubs = {
                "gh": GH_STUB,
                "git": '#!/bin/sh\n[ "$*" = "rev-parse HEAD" ] || exit 1\n'
                       + "printf '%s\\n' " + CANDIDATE + "\n",
                "sleep": "#!/bin/sh\nexit 0\n",
            }
            for name, source in stubs.items():
                path = root / "bin" / name
                path.write_text(source)
                path.chmod(0o755)
            env = dict(os.environ, PATH=str(root / "bin") + os.pathsep + os.environ["PATH"],
                       RELEASE_TEST_STATE=str(state_file), RELEASE_TAG="v1.1.0",
                       GH_REPO="fixture/repo", GH_TOKEN="unused-local-fixture",
                       RUNNER_TEMP=str(root))
            result = subprocess.run(["bash", str(SCRIPT)], cwd=root, env=env,
                                    text=True, capture_output=True, timeout=15)
            return result, json.loads(state_file.read_text())

    def test_verified_lightweight_and_annotated_tags_publish(self):
        for mode in ("lightweight", "annotated", "nested_annotated"):
            with self.subTest(mode=mode):
                result, state = self.run_release(mode)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue(state["published"])
                self.assertEqual(state["published_main"], CANDIDATE)
                self.assertEqual(state["published_commit"], CANDIDATE)
                self.assertGreaterEqual(state["polls"], 3)
                self.assertIn("--verify-tag", state["release_args"])
                for expected in (CANDIDATE, "https://example.invalid/main-ci",
                                 "https://example.invalid/tag-ci", "Fixture notes."):
                    self.assertIn(expected, state["notes"])

    def test_rv25_refs_changed_during_ci_block_publication(self):
        for mode in ("main_moves", "tag_moves", "annotated_tag_moves"):
            with self.subTest(mode=mode):
                result, state = self.run_release(mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertGreaterEqual(state["polls"], 3)
                self.assertFalse(state["published"])
                self.assertIn("Release refs changed", result.stderr)

    def test_missing_invalid_or_unreadable_refs_block_publication(self):
        for mode in ("main_api_error", "tag_deleted", "tag_object_api_error",
                     "malformed_tag", "non_commit_tag", "tag_cycle"):
            with self.subTest(mode=mode):
                result, state = self.run_release(mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(state["published"])
                self.assertLess(len(state["calls"]), 30)  # bounded tag resolution

    def test_failed_main_or_tag_ci_still_blocks_publication(self):
        for mode in ("main_ci_failure", "tag_ci_failure"):
            with self.subTest(mode=mode):
                result, state = self.run_release(mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(state["published"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
