## What does this change?

<!-- One or two sentences. Link the issue: "Fixes #123". -->

## Why?

<!-- The problem this solves. -->

## How did you test it?

- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] `make conformance` passes (needed for changes to the solver or the fake API)
- [ ] I added or updated tests
- [ ] I ran the end-to-end test against a real Contabo zone (only needed for changes to how records are read or written)

## Checklist

- [ ] Docs are updated (README, `docs/`, chart `values.yaml`) if behavior or settings changed
- [ ] New edge cases are listed in `docs/edge-cases.md`
- [ ] No secrets, tokens or personal data in code, tests, logs or this PR
- [ ] If I learned something new about the Contabo API, I updated `internal/contabo/contabotest` and `docs/contabo-api.md`
