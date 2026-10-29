# mugicrew

> This project is still in beta and may be subject to breaking changes.

`mugicrew` is a local-first agentic software engineering interface. It helps you refine and scope vague ideas and implement them in a systematic way. Your ability to build is only limited by your own ability to make effective decisions.

You can run `mugicrew` on any codebase with little to no modification. Just run `mugicrew init` in your workspace to set up your local issue tracker and you're ready to go. `mugicrew` will use your existing harness (note: beta currently only supports Claude Code). You don't have to replace your existing workflow either—you can try `mugicrew` for one feature or commit while using your existing workflows for others. `mugicrew` just offers another paradigm for working on your codebase. It works especially well with monorepo or nested repo setups.

`mugicrew` will always be free and open source.

## How it works

See the website for a full explanation of how `mugicrew` models the software engineering process.

## Getting started

> Currently `mugicrew` is only distributed via `npm`. More package repositories to come.

Install `mugicrew` globally.

```bash
npm i -g @jayulabs/mugicrew
```

Then run `mugicrew init` and `mugicrew doctor`; see [docs/install.md](docs/install.md).
The first human in a Workspace also runs `mugicrew init workspace`, which writes the
shared `.mugicrew/config.toml`; see
[First human in a Workspace](docs/install.md#first-human-in-a-workspace).

## Documentation

- [Install](docs/install.md): prerequisites, `mugicrew init`, `mugicrew doctor`, GitLab, Jira and backup setup
- [Upgrading](docs/upgrading.md): upgrading across the Errand rename
- [Workspace configuration](docs/configuration.md): contexts, Require Initiatives, the land gate, poll intervals, TUI key notes
- [Agent Profiles](docs/agent-profiles.md): running bead kinds on other models or endpoints
- [GitLab land strategy](docs/gitlab.md): landing through GitLab merge requests
- [Bd store mode](docs/bd-store.md): moving an embedded bd store to server mode
- [Rebasing and landing](docs/landing.md): `mugicrew rebase` and `mugicrew land-lineage`
- [Bead lifecycle](docs/bead-lifecycle.md): how each bead kind is created, closed and ordered
- [Glossary](GLOSSARY.md): the terms mugicrew uses
- [Architecture decisions](docs/adr/): mugicrew's ADRs
- [Developing mugicrew](docs/development.md): build and test
- [Releasing](docs/release.md): cutting an npm release

## Contributing

`mugicrew` does not accept external pull requests, but is open to feature requests and bug reports. Power users are also encouraged to fork `mugicrew` and customize it to suit their own needs (you can even use `mugicrew` to modify itself to great effect).

We also plan for `mugicrew` to be a key piece in a larger software ecosystem that will support distribution of customized software, so stay tuned!

## Acknowledgments

Many of the foundational agentic software engineering concepts implemented in `mugicrew` are inspired by Matt Pocock's exceptional agent skills repository—many of the `mugicrew` bead kinds are derivations and extensions of those skills.

This work is also partly inspired by Steve Yegge's Gas Town, and uses the same Beads package for agent-first issue tracking.

Thank you!
