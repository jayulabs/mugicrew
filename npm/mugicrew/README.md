# @jayulabs/mugicrew

> This project is still in beta and may be subject to breaking changes.

`mugicrew` is a local-first agentic software engineering interface. It helps you refine and scope vague ideas and implement them in a systematic way. Your ability to build is only limited by your own ability to make effective decisions.

You can run `mugicrew` on any codebase with little to no modification. Just run `mugicrew init` in your workspace to set up your local issue tracker and you're ready to go. `mugicrew` will use your existing harness (note: beta currently only supports Claude Code). You don't have to replace your existing workflow either—you can try `mugicrew` for one feature or commit while using your existing workflows for others. `mugicrew` just offers another paradigm for working on your codebase. It works especially well with monorepo or nested repo setups.

`mugicrew` will always be free and open source.

## Getting started

> Currently `mugicrew` is only distributed via `npm`. More package repositories to come.

Install `mugicrew` globally.

```bash
npm i -g @jayulabs/mugicrew
```

Then run `mugicrew init` and `mugicrew doctor`; see the [install doc](https://github.com/jayulabs/mugicrew/blob/main/apps/mugicrew/docs/install.md).
The first human in a Workspace also runs `mugicrew init workspace`, which writes the
shared `.mugicrew/config.toml`; see
[First human in a Workspace](https://github.com/jayulabs/mugicrew/blob/main/apps/mugicrew/docs/install.md#first-human-in-a-workspace).

For documentation, contributing and acknowledgments, see the
[full README on GitHub](https://github.com/jayulabs/mugicrew/blob/main/apps/mugicrew/README.md).
