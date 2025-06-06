# `ghavm`: the GitHub Actions Version Manager

[![Documentation](https://pkg.go.dev/badge/github.com/mccutchen/ghavm)](https://pkg.go.dev/github.com/mccutchen/ghavm)
[![Build status](https://github.com/mccutchen/ghavm/actions/workflows/test.yaml/badge.svg)](https://github.com/mccutchen/ghavm/actions/workflows/test.yaml)
[![Code coverage](https://codecov.io/gh/mccutchen/ghavm/branch/main/graph/badge.svg)](https://codecov.io/gh/mccutchen/ghavm)
[![Go report card](http://goreportcard.com/badge/github.com/mccutchen/ghavm)](https://goreportcard.com/report/github.com/mccutchen/ghavm)

`ghavm` is a tool for managing version pinning and upgrades for GitHub
Actions workflows, to ease the burden of following security best practices.

In particular, per GitHub's own [Security Hardening for GitHub Actions][gh-sec]
documentation:

> [!IMPORTANT]
> _Pinning an action to a full length commit SHA is currently the only way to
> use an action as an immutable release. Pinning to a particular SHA helps
> mitigate the risk of a bad actor adding a backdoor to the action's
> repository_

See [Security best practics](#security-best-practices) below for more
information on the specific problem `ghavm` helps solve.


## TLDR

First, you might use `ghavm pin` to **pin your actions to immutable commit
hashes**, ensuring that they'll run the exact same code every time:

```bash
$ go run github.com/mccutchen/ghavm@latest pin
```
```diff
--- example.before.yaml
+++ example.pinned.yaml
@@ -6,22 +6,22 @@
   test:
     runs-on: ubuntu-latest
     steps:
-      - uses: actions/setup-go@v3.5.0
+      - uses: actions/setup-go@6edd4406fa81c3da01a34fa6f6343087c207a568 # v3.5.0
         with:
           version: stable

-      - uses: actions/checkout@v4.1.0
+      - uses: actions/checkout@8ade135a41bc03ea155e62e844d188df1ea18608 # v4.1.0
         with:
           fetch-depth: 1

-      - uses: golangci/golangci-lint-action@v6.5.0
+      - uses: golangci/golangci-lint-action@2226d7cb06a077cd73e56eedd38eecad18e5d837 # v6.5.0
         with:
           version: latest

       - name: test
         run: make testci

-      - uses: codecov/codecov-action@v4.2.0
+      - uses: codecov/codecov-action@7afa10ed9b269c561c2336fd862446844e0cbf71 # v4.2.0
         with:
           token: ${{ secrets.CODECOV_TOKEN }}
           fail_ci_if_error: true
```

Eventually, you may need to upgrade your actions to newer releases, which
can be a labor-intensive and potentially error-prone process when manually
juggling long commit hashes.

The `ghavm upgrade` makes it easy to **upgrade a workflow's actions** to their
latest "compatible" release or latest available release:

```bash
$ go run github.com/mccutchen/ghavm@latest upgrade --mode=latest
```
```diff
--- example.pinned.yaml
+++ example.upgraded.yaml
@@ -6,22 +6,22 @@
   test:
     runs-on: ubuntu-latest
     steps:
-      - uses: actions/setup-go@6edd4406fa81c3da01a34fa6f6343087c207a568 # v3.5.0
+      - uses: actions/setup-go@d35c59abb061a4a6fb18e82ac0862c26744d6ab5 # v5.5.0
         with:
           version: stable

-      - uses: actions/checkout@8ade135a41bc03ea155e62e844d188df1ea18608 # v4.1.0
+      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2
         with:
           fetch-depth: 1

-      - uses: golangci/golangci-lint-action@2226d7cb06a077cd73e56eedd38eecad18e5d837 # v6.5.0
+      - uses: golangci/golangci-lint-action@4afd733a84b1f43292c63897423277bb7f4313a9 # v8.0.0
         with:
           version: latest

       - name: test
         run: make testci

-      - uses: codecov/codecov-action@7afa10ed9b269c561c2336fd862446844e0cbf71 # v4.2.0
+      - uses: codecov/codecov-action@18283e04ce6e62d37312384ff67231eb8fd56d24 # v5.4.3
         with:
           token: ${{ secrets.CODECOV_TOKEN }}
           fail_ci_if_error: true
```

> [!TIP]
> Pass `--target`/`-t` to limit pinning or upgrading to one or more specific
> actions, instead of operating on every action at once.


## Installation

Until we publish prebuilt binaries, you'll need a Go toolchain to install
`ghavm`:

```bash
go install github.com/mccutchen/ghavm@latest
```

Or, run it directly without explicit installation:

```bash
go run github.com/mccutchen/ghavm@latest
```


## Usage

```
$ ghavm --help
```
```
ghavm manages version pinning and upgrades for GitHub Actions workflows.

Usage:
  ghavm [command]

Available Commands:
  list        List current action versions and available upgrades
  pin         Pin current action versions to immutable commit hashes
  upgrade     Upgrade and re-pin action versions according to --mode

Flags:
  -h, --help   help for ghavm

Use "ghavm [command] --help" for more information about a command.
```


## Security best practices

### Background

- Per the [Using third-party actions][gh-sec-3p] section of GitHub's own
  [Security Hardening for GitHub Actions][gh-sec] doc:

  > The individual jobs in a workflow can interact with (and compromise) other
  > jobs. For example, a job querying the environment variables used by a later
  > job, writing files to a shared directory that a later job processes, or
  > even more directly by interacting with the Docker socket and inspecting
  > other running containers and executing commands in them.
  >
  > This means that a compromise of a single action within a workflow can be
  > very significant, as that compromised action would have access to all
  > secrets configured on your repository, and may be able to use the
  > GITHUB_TOKEN to write to the repository. Consequently, there is significant
  > risk in sourcing actions from third-party repositories on GitHub.

  and

  > **Pinning an action to a full length commit SHA is currently the only way
  > to use an action as an immutable release. Pinning to a particular SHA helps
  > mitigate the risk of a bad actor adding a backdoor to the action's
  > repository**, as they would need to generate a SHA-1 collision for a valid
  > Git object payload. When selecting a SHA, you should verify it is from the
  > action's repository and not a repository fork.

- For a good overview of potential attacks enabled by unpinned action versions
  and a walkthrough of various other ways to manage pinned action versions, see
  [Pinning GitHub Actions for Enhanced Security: Everything You Should
  Know][step-sec] from StepSecurity.

- And take a look at Wiz's [guide to GitHub Actions security
  hardening][wiz-sec], which includes an overview of recent high-profile GitHub
  Actions-based supply chain attacks, some of which may have been mitigated by
  immutably pinned action versions.

### Version pinning does not solve every problem

While pinning the action versions to immutable commit hashes is a critical step
in securing GitHub Actions workflows, it does not perfectly mitigate the risks.

In particular, per [the Wiz hardening guide][wiz-sec]:

> It is important to consider **transitive risk**: even if you hash pin an
> Action, if it relies on another Action with weaker pinning, you're still
> exposed.

So, even when pinning your action versions with `ghavm` or any other tool, it
is important to continue following best practices, _especially_ when using 3rd
party actions not owned by GitHub itself.

### Further reading

- GitHub's own [Security hardening for GitHub Actions][gh-sec] guide
- StepSecurity's [7 GitHub Actions Security Best Practices][step-sec] guide
- GitGuardian's [GitHub Actions Security Best Practices][gg-sec] guide


## Prior art and other tools

There are a number of other tools offering similar functionality:

- [mheap/pin-github-action][] inspired this project! It offers the same
  functionality as the `ghavm pin` command, but does not seem to offer the
  ability to then upgrade pinned versions over time.

- [dependabot][] has native support for managing GitHub Actions versions with
  the same kind of pinning that this project provides, and can be configured to
  automatically open pull requests to update those versions over time.

  **Most users should probably consider dependabot instead of `ghavm` to
  actually automate version-pinned security updates!**


## License

MIT

[gh-sec]: https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions
[gh-sec-3p]: https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions
[wiz-sec]: https://www.wiz.io/blog/github-actions-security-guide
[step-sec]: https://www.stepsecurity.io/blog/pinning-github-actions-for-enhanced-security-a-complete-guide
[gg-sec]: https://blog.gitguardian.com/github-actions-security-cheat-sheet/
[mheap/pin-github-action]: https://github.com/mheap/pin-github-action
[dependabot]: https://docs.github.com/en/code-security/dependabot/ecosystems-supported-by-dependabot/supported-ecosystems-and-repositories#github-actions
