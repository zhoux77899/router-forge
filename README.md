# Router Forge

Router Forge is a project for controlling and evaluating AI model routing policies.
Its intended workflow is to adjust a policy, control request dispatch, run repeatable
agent evaluations, compare success rates and costs, and publish a tested policy version.

## Status

The repository is in its initialization stage. It contains project documentation,
contribution templates, and repository validation tools. The routing system, control
plane, evaluation workers, and user interfaces are planned. There is no runnable
product or application test suite yet.

Start with [CONTRIBUTING.md](CONTRIBUTING.md) to prepare the repository tooling or
propose a change.

## Target workflow

1. Define model-selection constraints and preferences, such as capability, cost,
   latency, and availability.
2. Edit and version routing policies, with a record of candidate models, selection
   reasons, and fallback decisions.
3. Run agent evaluations under fixed model or frozen-policy conditions so results
   can be attributed to the tested configuration.
4. Compare task outcomes, request usage, cost, and latency before publishing a policy.

## Target architecture

| Module | Responsibility |
| --- | --- |
| Control plane | Manage provider configuration, policy versions, evaluation jobs, and result queries. |
| Model gateway | Dispatch model requests and record routing decisions, usage, cost, latency, and errors. |
| Evaluation worker | Execute isolated agent tasks, run independent scoring, and record runs, tasks, and attempts. |
| Desktop console | Configure providers and credentials, edit policies, and start or control evaluations. |
| Web dashboard | Compare evaluation results and inspect request costs, execution traces, and failure details. |

Configuration, per-request metering, and evaluation results have separate
responsibilities. Request identifiers connect model calls to evaluation runs, tasks,
and attempts. A task can make many model calls; its score and its request costs need
separate records.

## Repository guide

| Location | Contents |
| --- | --- |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Commit and PR rules, issue reporting, tooling setup, and validation commands. |
| [AGENTS.md](AGENTS.md) | Entry point for coding-agent guidance. |
| [docs/agents](docs/agents) | Issue tracker, triage, and domain-documentation conventions. |
| [.github](.github) | PR template, issue forms, and the PR-title workflow. |
| [.githooks](.githooks) | Opt-in commit-message hook, message checker, and commitlint configuration. |
| [package.json](package.json) | Development tooling only, not a product application or workspace. |

## Contributing

Use the [issue chooser](https://github.com/zhoux77899/router-forge/issues/new/choose)
to report a bug or propose a feature. Bug reports ask for reproduction steps,
expected results, and actual results. Feature requests ask for the problem and a
proposed solution. Blank issues remain available.

Read the [contribution guide](CONTRIBUTING.md) before opening a PR. Coding agents
should also read [AGENTS.md](AGENTS.md).

## Design reference

The [project design discussion](https://chatgpt.com/share/6ae15193-8830-83ee-b320-d8237189a48b)
describes the routing-control and evaluation goals behind this repository.

## License

No license file is currently included in this repository.
