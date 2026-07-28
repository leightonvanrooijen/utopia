# ANTHROPIC_AUTH_TOKEN can be set but never authenticates anything

Found while implementing the `anthropic-env-file` work item of the
claude-authentication-credential-selection CR (2026-07).

`AuthTokenEnvVar` (internal/domain/auth.go) exists in the codebase only to be
**removed**. Every code path that touches it drops it:

- `APIKeyCredential.Env` - removes it so the resolved key is the only credential
  (sending both is a 401).
- `SubscriptionEnv` - removes it so the claude CLI falls through to OAuth.
- `ApplyEnvFile` - skips it, so a token in .utopia/.env is not reapplied on top
  of the resolved key.

`parseAnthropicEnvFile` happily loads it - `ANTHROPIC_AUTH_TOKEN` matches the
prefix filter, and there's a test asserting the map keeps it
(internal/credentials_test.go, "keeps other anthropic variables"). But no auth
mode consumes it, so writing it into .utopia/.env is silently inert.

## Why this matters

Same shape as `models-config-validated-but-unconsumed`: a value that parses
cleanly and does nothing reads as a working setting. Someone with a bearer token
rather than a long-lived key has no working mode - `auth.mode: api-key` demands
`ANTHROPIC_API_KEY` and errors with `MissingAPIKeyError` even when a valid token
sits in the file right next to it.

The two plausible resolutions:

1. A third auth mode (`auth-token`?) that resolves `ANTHROPIC_AUTH_TOKEN`
   file-first and drops `ANTHROPIC_API_KEY` - the mirror image of api-key mode.
   `CredentialSource` and the resolution/logging machinery already generalise.
2. Widen api-key mode to accept either credential, preferring whichever the file
   defines. Cheaper, but "api-key" then names something that isn't only a key.

Not a bug against any current spec - no feature claims token support - so this is
a new-feature CR against spec `claude-authentication`, not a fix. Worth deciding
deliberately rather than leaving the variable half-present.
