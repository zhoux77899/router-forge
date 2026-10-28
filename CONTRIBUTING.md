# Contributing

Router Forge is in its initialization stage. Contributions currently establish
project documentation and repository tooling. Discuss product behavior in an issue
before proposing a substantial change. Keep unrelated work in separate commits.

## Prepare the repository tooling

Use Git, Node.js 22.12.0 or newer, and npm. From the repository root, install the
locked development dependencies:

```text
npm ci
```

The npm manifest is for contribution tooling only. There is no product application
build or startup command yet. Installing dependencies does not enable Git hooks or
change your Git configuration.

### Enable the local commit-message hook

Check for an existing hooks configuration first:

```text
git config --show-origin --get core.hooksPath
```

No output means no hooks path is configured. If a path is already configured,
review and integrate its checks before changing it. Do not silently replace an
existing hook setup.

To enable this repository's hook for the current clone:

```text
git config --local core.hooksPath .githooks
```

On a POSIX system, also make the hook executable:

```sh
chmod +x .githooks/commit-msg
```

Git for Windows supplies the shell needed to run the hook. The targeted
`.gitattributes` rule keeps the hook's line endings as LF.

The hook, its Node.js message checker, and the commitlint configuration live in
`.githooks`. The checker removes Git template comments and verbose diffs, rejects
empty cleaned messages, and calls the installed commitlint CLI. Cleanup helpers
and commit rules are in `.githooks/commitlint.config.mjs`. Missing Node.js, npm, or
development dependencies cause a clear failure; the hook never downloads a
replacement tool or silently skips validation.

### Optional commit template

Enable the editing guide for this clone if you want it:

```text
git config --local commit.template .gitmessage
```

The template contains comments, not a ready-to-submit message. Write the title and
body yourself, and use `git commit -s` to add a sign-off from your configured Git
identity. Template comments use Git's default `#` comment marker.

## Commit messages

Use this structure for every commit:

```text
type(scope): imperative description

Explain the motivation or implementation details in prose.

Signed-off-by: Contributor Name <contributor@example.com>
```

For example:

```text
docs(repo): document the contribution process

Explains how contributors enable local checks and report their validation results.

Signed-off-by: Contributor Name <contributor@example.com>
```

The scope, description, explanatory body, and sign-off are all required. Separate
the title, body, and trailers with blank lines. Use English for the title and body,
with a concise imperative description. Scope names should be short and stable,
such as `policy`, `gateway`, `eval`, `api`, `docs`, or `repo`. They must not be blank
or padded with whitespace; they are not restricted to a fixed module list.

| Type | Use | PR tag |
| --- | --- | --- |
| `feat` | Introduce a feature or behavior. | `Feat` |
| `fix` | Correct a bug. | `Fix` |
| `docs` | Change documentation. | `Docs` |
| `style` | Change formatting without changing behavior. | `Style` |
| `refactor` | Restructure code without adding a feature or fixing a bug. | `Refactor` |
| `test` | Add or change tests. | `Test` |
| `chore` | Maintain supporting tools or repository housekeeping. | `Chore` |
| `perf` | Improve performance. | `Perf` |
| `build` | Change build tooling or external build dependencies. | `Build` |
| `revert` | Undo an earlier change. | `Revert` |
| `ci` | Change CI configuration or scripts. | `CI` |
| `deps` | Update dependencies. | `Deps` |

Commit types are declared in `.githooks/commitlint.config.mjs`; PR tags are declared
separately in `.github/workflows/pr-title.yml`. When changing a type, update both
lists and this table.

Comments and Git trailers are not an explanatory body. Use ordinary sentences or
paragraphs; a standalone `Key: value` block may be interpreted as Git trailers.
A title followed only by `Signed-off-by` fails validation, as does a body made only
of trailers. A sign-off must be a final trailer with a name and email address;
mentioning `Signed-off-by` in prose does not count. Additional trailers, including
`Co-authored-by`, are allowed but do not replace the sign-off.

`Signed-off-by` is a contributor's sign-off, not a GPG signature. Use your own
configured identity; tooling cannot establish whether the declaration is true.
The hook checks structure and required content. Reviewers still check English
wording, imperative style, and whether the body explains the change adequately.
No additional title-length limit or subject-capitalization rule is enforced.

Merge, revert, and release-like messages are not exempt. Edit automatically
prepared messages to follow the same structure. CI does not check commit history,
and local hooks do not cover commits created through GitHub's web interface.
Contributors and maintainers remain responsible for those messages.

## Pull requests

Use `[Type] Description` for PR titles, with the case-sensitive tag from the table:

```text
[Docs] Add contribution guidance
```

The [PR template](.github/PULL_REQUEST_TEMPLATE.md) provides `Summary`, `Test`, and
`Checklist`. Explain what changed and why, and link a relevant issue when one
exists. In `Test`, report the commands or checks actually completed. If a check
was not run, state why and describe the remaining risk. Do not mark a check as
passed merely because it is listed in the template.

The `PR title` workflow checks only the title. It does not inspect commit messages,
PR body sections, or checklist completion. Opening, editing, reopening, updating,
or marking a PR ready for review triggers a title check.

The check is JavaScript inline in the trusted workflow definition. It runs with
read-only permissions, without checking out the repository or installing project
dependencies. It treats the `PR_TITLE` environment variable as data and does not
execute PR-head code.

The workflow becomes available after it is published to the default branch.
Maintainers can make the `PR title` check required in branch protection; this
change does not configure that protection automatically.

### Squash merges

The PR title is not a valid commit title. Before completing a squash merge, change
its generated commit message to `type(scope): description`, write an explanatory
body, and retain an appropriate `Signed-off-by` trailer. The local hook is not run
by GitHub's squash UI, so the maintainer must review the final message.

## Issues

Use the [issue chooser](https://github.com/zhoux77899/router-forge/issues/new/choose)
for bugs and feature requests. Bug reports require reproduction steps, expected
results, and actual results. Feature requests require the problem and proposed
solution. Optional fields provide room for context; blank issues remain enabled.

Remove credentials, private prompts, sensitive repository content, and private
logs before submitting an issue. The templates do not create labels or assign
reviewers. Existing tracker conventions are in
[docs/agents/issue-tracker.md](docs/agents/issue-tracker.md).

## Local files and shared instructions

The first three `.gitignore` groups exclude IDE, editor, and agent-harness
directories. Harness directories such as `.claude`, `.cursor`, `.copilot`,
`.codex`, and `.agents` are ignored in full, including their rules, skills, and
configuration. The remaining groups cover worktrees and job artifacts, environment
files, dependencies, caches and test output, logs, and operating-system files.

Keep shared project guidance in root `AGENTS.md` or `CLAUDE.md`, or under `.github`.
Root `.editorconfig`, `.github`, `.githooks`, and `docs/agents` remain eligible for
version control. These locations are distinct from ignored harness directories.

Only `.env.example` is exempt from the environment-file exclusions. Any such
example must use placeholders, never real credentials. Ignore rules do not remove
already tracked files or provide secret scanning. Lockfiles, product configuration,
documentation, and benchmark inputs outside ignored harness directories remain
eligible for version control.

## Validation

Repository infrastructure has no maintained test suite. When changing hook or
workflow logic, use temporary examples to check the affected acceptance and
rejection cases. Start with syntax and whitespace checks:

```text
node --check .githooks/commitlint.config.mjs
node --check .githooks/check-commit-message.mjs
git diff --check
```

To check a message file directly:

```text
npm run lint:commit -- message.txt
```

For PR-title changes, exercise the inline JavaScript in
`.github/workflows/pr-title.yml` with valid and invalid `PR_TITLE` values. There is
no separate npm command for PR-title validation. Report the workflow's check
result when it is available on the PR.

Use the smallest relevant checks for your change, and record their actual results
in the PR.

## Coding agents

Read [AGENTS.md](AGENTS.md) before editing. Do not include local harness state in a
change, forge a sign-off, or claim to have run checks that were not executed.

## References

Commit and PR conventions are adapted from Luma's
[contribution guide](https://raw.githubusercontent.com/zhoux77899/luma/refs/heads/main/CONTRIBUTING.md)
and [PR template](https://raw.githubusercontent.com/zhoux77899/luma/refs/heads/main/.github/PULL_REQUEST_TEMPLATE.md).
