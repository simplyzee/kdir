# Security policy

kdir decides which cluster and namespace kubectl talks to, so a bug that sends a
command to the wrong cluster is a security bug, not just a correctness one.

## Reporting a vulnerability

Please **don't open a public issue**. Report it privately through
[GitHub's private vulnerability reporting](https://github.com/thomaschaplin/kdir/security/advisories/new).

Include the kdir version (`kdir version`), your OS and shell, and the steps or
`.kubectx` that reproduce it. You should get a reply within a week. Once a fix
is released, the advisory is published with credit to you unless you'd rather
stay anonymous.

## What counts

Examples of what we want to hear about:

- An untrusted (not `kdir allow`ed) `.kubectx`, or one edited after it was
  allowed, taking effect.
- kdir failing **open**: kubectl reaching a cluster when the project's context
  is missing or ambiguous, instead of refusing to run.
- kdir writing to your own kubeconfig files.
- A `.kubectx` or a directory name that makes the shell hook run arbitrary
  commands.
- `protect: true` being bypassed for a mutating kubectl command it should catch.
- Overlay files readable or writable by other users.

## Supported versions

Only the latest release gets security fixes.
