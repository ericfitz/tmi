---
name: file-client-bug
description: File a bug report against the TMI client (tmi-ux) when a problem is identified as a client-side issue. Creates a GitHub issue in ericfitz/tmi-ux, adds it to the TMI project, and sets status to In Progress.
allowed-tools: Bash, Read, Glob, Grep
---

# File Client Bug Skill

You are filing a bug report against the TMI client application (https://github.com/ericfitz/tmi-ux) for a problem discovered while working on the TMI server.

## Prerequisites

Before invoking this skill, you should already have:

- A clear understanding of the bug (what the client is doing wrong)
- Evidence supporting the conclusion that this is a client-side issue
- Any relevant context from the server-side investigation

## Step 1: Gather Context

1. Get the current branch name:

   ```bash
   git branch --show-current
   ```

2. Determine if there is a matching milestone in tmi-ux:

   ```bash
   gh api repos/ericfitz/tmi-ux/milestones --jq '.[] | select(.title == "<branch-name>") | .number'
   ```

   - If the current branch is `main`, skip milestone assignment.
   - If a milestone matching the branch name exists, note its number.

3. If the bug was discovered via a GitHub issue or PR, note the issue/PR number and repo so it can be cross-referenced.

## Step 2: Create the Issue

Create a detailed, unambiguous bug report using `gh issue create`. The report must include all of the following sections:

```bash
gh issue create --repo ericfitz/tmi-ux \
  --title "<concise bug title>" \
  --label "bug" \
  [--milestone "<milestone-name>" if matching milestone exists] \
  --body "$(cat <<'ISSUE_EOF'
## Bug Report

### Summary
<One-sentence description of the bug>

### Expected Behavior
<What the client should do>

### Actual Behavior
<What the client actually does>

### Evidence
<Specific evidence from server-side investigation that supports this being a client bug. Include:
- Relevant API request/response details
- Server log excerpts if applicable
- Protocol or specification references
- Any test results that demonstrate the issue>

### Suggested Fix
<Specific suggestions for what the client should do differently. Be precise about:
- Which API endpoint(s) are affected
- What the correct request format/behavior should be
- Any relevant OpenAPI spec references>

### Server-Side Context
<If this bug was discovered while working on a server-side issue, include:
- Link to the original issue (e.g., ericfitz/tmi#123)
- Brief explanation of how the client bug was discovered
- Any server-side changes that may have exposed the client bug>

### Reproduction Steps
1. <Step-by-step instructions to reproduce>
2. <Include specific API calls, user actions, or test scenarios>

### Environment
- TMI Server Branch: <current branch>
- TMI Server Commit: <current HEAD short hash>
ISSUE_EOF
)"
```

## Step 3: Add to TMI Project and Set Status

After creating the issue, add it to the TMI project and set its status to "In Progress":

```bash
# Get the issue node ID from the URL returned by gh issue create
ISSUE_URL="<url returned by gh issue create>"
ISSUE_NUMBER=$(echo "$ISSUE_URL" | grep -oE '[0-9]+$')
ISSUE_NODE_ID=$(gh api repos/ericfitz/tmi-ux/issues/$ISSUE_NUMBER --jq '.node_id')

# Add to TMI project (project number 2, owner ericfitz)
ITEM_ID=$(gh project item-add 2 --owner ericfitz --url "$ISSUE_URL" --format json | jq -r '.id')

# Set status to "In Progress" (field ID: PVTSSF_lAHOACjZhM4BC0Z1zg06000, option ID: 47fc9ee4)
gh project item-edit --project-id PVT_kwHOACjZhM4BC0Z1 --id "$ITEM_ID" --field-id PVTSSF_lAHOACjZhM4BC0Z1zg06000 --single-select-option-id 47fc9ee4
```

## Step 4: Report Back

After filing the bug, report to the user:

- The issue URL
- The issue number
- Confirmation that it was added to the TMI project with "In Progress" status
- Whether a milestone was assigned

## Important Notes

- Always use the `bug` label on the issue.
- The bug report must be self-contained: a tmi-ux developer should be able to understand and act on it without needing to ask questions.
- Include enough server-side context that the client developer can find the original investigation if needed.
- Do not file duplicate issues. Before creating, search for existing issues:
  ```bash
  gh issue list --repo ericfitz/tmi-ux --state open --search "<keywords>" --json number,title
  ```
