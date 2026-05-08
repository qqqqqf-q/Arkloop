# LLM Retry Tuning Design

## Summary

Adjust the worker-side default LLM retry behavior in two ways:

1. Increase the default `llm.retry.max_attempts` from `3` to `10`.
2. Replace the current deterministic exponential backoff with jittered exponential backoff.

This change applies to the shared worker retry path and therefore covers Ark CLI related local-provider runs as well, because they reuse the same `runengine -> agent loop` retry configuration.

## Goals

- Allow more recovery attempts for transient provider failures.
- Reduce synchronized retry bursts when many runs fail at the same time.
- Preserve existing retry semantics:
  - only retry `provider.retryable`
  - stop once max attempts are exhausted
  - keep the configured base delay and overall cap behavior

## Non-Goals

- Do not change which provider errors are classified as retryable.
- Do not add per-provider retry policies in this change.
- Do not change user-visible event names or terminal state semantics.

## Design

### Default Attempts

Update the default platform config entry for `llm.retry.max_attempts` from `3` to `10`.

The meaning remains "total attempts", not "extra retries". With the new default:

- attempt 1: initial request
- attempts 2-10: retry attempts

### Backoff Strategy

Replace the current fixed exponential delay:

- `delay = base * 2^(attempt-1)`

with jittered exponential backoff:

- `cap = min(maxDelay, base * 2^(attempt-1))`
- `delay = random(0, cap)`

This is effectively full jitter. It keeps the exponential growth envelope while spreading retries across time.

### Cap And Safety

- Preserve the existing hard max delay cap of `60s`.
- Keep behavior deterministic at the API contract level:
  - emitted event shape stays the same
  - `delay_ms` remains the actual chosen wait duration

## Testing

Update focused worker tests to cover:

- new default config value of `10`
- retry loop still stops after max attempts
- computed jittered delay is always within `0..cap`
- capped behavior still respects the `60s` ceiling

## Risks

- More attempts can increase total wait time for persistent failures.
- Jitter makes exact delays non-deterministic, so tests should validate ranges instead of fixed values.
