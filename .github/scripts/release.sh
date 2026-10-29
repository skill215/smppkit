#!/usr/bin/env bash
# SPDX-AI-Disclosure: ai-generated
# SPDX-AI-Model: GPT-6.0 Astra
# SPDX-AI-Provider: OpenAI
# SPDX-AI-Scope: Release automation generated and revised under maintainer direction.
# SPDX-AI-Date: 2026-10-29

set -euo pipefail
[[ "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]
notes="docs/releases/$RELEASE_TAG.md"
test -s "$notes"
sha=$(git rev-parse HEAD)

# A lightweight tag points directly to a commit; annotated tags point to tag
# objects, which can in turn point to other tag objects. Peel a bounded chain
# using the Git database API, rather than comparing an annotated tag's own ID.
remote_tag_commit() {
  local object kind oid depth=0
  object=$(gh api "repos/$GH_REPO/git/ref/tags/$RELEASE_TAG" --jq '.object') || return 1
  while :; do
    kind=$(jq -er '.type' <<< "$object") || return 1
    oid=$(jq -er '.sha | select(type == "string" and test("^[0-9a-f]{40}$"))' <<< "$object") || return 1
    case "$kind" in
      commit) printf '%s\n' "$oid"; return ;;
      tag)
        if (( depth >= 16 )); then
          echo 'Release tag exceeds 16 annotated-tag dereferences' >&2
          return 1
        fi
        object=$(gh api "repos/$GH_REPO/git/tags/$oid" --jq '.object') || return 1
        depth=$((depth + 1))
        ;;
      *) echo 'Release tag does not resolve to a commit' >&2; return 1 ;;
    esac
  done
}

main_sha=$(gh api "repos/$GH_REPO/git/ref/heads/main" --jq '.object.sha')
test "$sha" = "$main_sha"
cp "$notes" "$RUNNER_TEMP/release-notes.md"
printf '\n## Verified publication\n\nCommit: `%s`\n' "$sha" >> "$RUNNER_TEMP/release-notes.md"
# Both main and tag pushes run the existing Go matrix. Never turn a
# failed check into a release by retrying jobs or ignoring a result.
for branch in main "$RELEASE_TAG"; do
  passed=false
  # Count-budget fuzzing can outlast the old ten-minute wait. Allow
  # the Go workflow's 45-minute limit plus scheduling overhead;
  # this polls status only and never reruns a failed workflow.
  for attempt in $(seq 1 300); do
    run=$(gh api --method GET "repos/$GH_REPO/actions/workflows/ci.yml/runs" \
      -f head_sha="$sha" -f event=push -f per_page=100 |
      jq --arg branch "$branch" '[.workflow_runs[] | select(.head_branch == $branch)] | sort_by(.run_number) | last // {}')
    if [[ $(jq -r '.status' <<< "$run") == completed ]]; then
      test "$(jq -r '.head_sha' <<< "$run")" = "$sha"
      test "$(jq -r '.conclusion' <<< "$run")" = success
      printf '\n- %s CI: %s\n' "$branch" "$(jq -r '.html_url' <<< "$run")" >> "$RUNNER_TEMP/release-notes.md"
      passed=true
      break
    fi
    sleep 10
  done
  test "$passed" = true
done

# CI can take minutes. Read the remote refs again immediately before publishing;
# --verify-tag only checks existence, not whether the tag still names our SHA.
# Assignments propagate errors under -e. The resolver explicitly returns on
# API/JSON errors because Bash normally clears errexit in command substitutions.
main_sha=$(gh api "repos/$GH_REPO/git/ref/heads/main" --jq '.object.sha')
tag_sha=$(remote_tag_commit)
if [[ "$main_sha" != "$sha" || "$tag_sha" != "$sha" ]]; then
  echo 'Release refs changed: main and tag must both match the CI-verified commit' >&2
  exit 1
fi
gh release create "$RELEASE_TAG" --repo "$GH_REPO" --verify-tag \
  --title "smppkit $RELEASE_TAG" --notes-file "$RUNNER_TEMP/release-notes.md" --latest
