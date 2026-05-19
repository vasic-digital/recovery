# Recovery — Application-level Fault Tolerance for Go

`digital.vasic.recovery` is a small, deliberately-scoped Go module that
provides **named circuit breakers**, **periodic health checking**, and
a **unified resilience facade** for Go services that need to degrade
gracefully when downstream dependencies misbehave.

It composes (and does not duplicate) the lower-level
`digital.vasic.concurrency/pkg/breaker` engine, adding the
application-level surface most services actually want: structured
logging hooks, named identification across many breakers, state-change
callbacks, periodic background health checks, and a single struct
(`facade.Resilience`) that ties the registry + checkers together
behind one API.

**Module ID**: `digital.vasic.recovery`
**Module type**: leaf submodule (no own-org runtime dependencies beyond `digital.vasic.concurrency`).
**Layout**: project-not-aware, reusable, modular — fully decoupled per CONST-051(B).

---

## Packages

- **`pkg/breaker`** — `CircuitBreaker` (named, logged, callback-aware
  wrapper around the concurrency engine) and `CircuitBreakerManager`
  (thread-safe registry that gives every caller a deterministic
  lookup-or-create helper for breakers keyed by name).
- **`pkg/health`** — `Checker` (ticker-driven background poller that
  calls a `CheckFunc` on a configurable interval and tracks the
  resulting `Status` (`StatusUnknown`, `StatusHealthy`,
  `StatusUnhealthy`) plus `LastError()` and `LastCheck()` accessors).
- **`pkg/facade`** — `Resilience` struct that composes
  `CircuitBreakerManager` and a name-indexed map of `Checker`
  instances behind a single API: `Execute(name, fn) error`,
  `GetOrCreateBreaker(name, cfg)`, `AddHealthCheck(name, fn,
  interval)`, `Stats() map[string]interface{}`, `Stop()`.

---

## Quick start

```go
package main

import (
    "errors"
    "fmt"
    "net/http"
    "time"

    "digital.vasic.recovery/pkg/facade"
)

func main() {
    res := facade.New(nil) // nil logger = silent; supply your own to receive Info/Warn/Debug.
    defer res.Stop()

    // Register a periodic health probe.
    res.AddHealthCheck("payment-api", func() error {
        resp, err := http.Get("https://api.example.com/healthz")
        if err != nil {
            return err
        }
        resp.Body.Close()
        if resp.StatusCode >= 500 {
            return errors.New("upstream 5xx")
        }
        return nil
    }, 10*time.Second)

    // Execute a guarded call. After enough consecutive failures the
    // breaker named "payment-api" trips open and Execute returns
    // immediately with the engine's "circuit open" error until the
    // configured reset timeout elapses.
    err := res.Execute("payment-api", func() error {
        resp, err := http.Get("https://api.example.com/charge")
        if err != nil {
            return err
        }
        resp.Body.Close()
        return nil
    })
    fmt.Printf("guarded call: err=%v stats=%v\n", err, res.Stats())
}
```

---

## Anti-bluff guarantees (CONST-035 / Article XI §11.9)

Recovery's tests AND Challenge runner are designed to fail loudly if
the published primitives drift away from the documented contract.
Every guarantee below carries positive runtime evidence — no metadata-
only PASS, no configuration-only PASS, no absence-of-error PASS.

| Guarantee | Where it is enforced |
|---|---|
| `breaker.NewCircuitBreaker(cfg)` returns a non-nil, closed breaker; `Execute(nilFn)` succeeds. | `challenges/runner/main.go` invariant 1, per-locale (5 locales = 5 PASS lines). |
| After `MaxFailures` consecutive failed `Execute` calls the breaker transitions to `StateOpen` (cannot ship a stub breaker that never trips). | Invariant 2 + paired mutation `RECOVERY_MUTATE_RUNNER=1` that flips polarity and asserts the runner exits non-zero. |
| `breaker.Reset()` returns a tripped breaker to `StateClosed` and zeroes `GetFailures()`. | Invariant 3, per-locale. |
| `CircuitBreakerManager.GetAll()` returns every registered breaker. | Runner asserts cardinality matches fixture count. |
| `health.NewChecker` + `Start(ctx)` reaches `StatusHealthy` when the `CheckFunc` returns nil and `StatusUnhealthy` (with non-nil `LastError()`) when it returns an error. | Invariant 4, both healthy + unhealthy paths exercised per locale. |
| `facade.Resilience.Execute(name, fn)` returns nil for a happy-path fn; the registered breaker surfaces in `Stats()` under the `"breakers"` key. | Invariant 5, per-locale; runner verifies every fixture's endpoint name appears in the stats map. |
| Human-readable rendering is locale-aware (5 locales: `en`, `sr`, `ja`, `es`, `de`). | Invariant 6: every fixture's banner must be UNIQUE; a collapsed-banner regression FAILs immediately (CONST-046). |
| Paired-mutation guard. | `challenges/recovery_describe_challenge.sh mutate` injects the polarity flip, runs the runner, asserts the runner exits non-zero (= mutation correctly detected), wrapper emits exit code 99 (paired-mutation success). If the runner exits 0 under mutation, the wrapper FAILs the whole Challenge — proves the assertions are real, not bluffs. |

---

## Running

```bash
# Unit tests (mocks allowed only here per CONST-050(A)).
go test ./... -count=1 -race

# Challenge runner — end-to-end, real primitives, 5 locales,
# bilingual+ render, captured PASS lines.
go run ./challenges/runner/

# Full Challenge with paired-mutation guard.
bash ./challenges/recovery_describe_challenge.sh normal   # → exit 0
bash ./challenges/recovery_describe_challenge.sh mutate   # → exit 99
```

Expected normal-mode output ends with `=== Summary: PASS=44 FAIL=0 ===`
followed by the EN + SR success vocabulary. The mutate leg ends with
`MUTATION DETECTED  (runner rc=1 → exit 99)` and exits 99. Any other
combination is a violation of the anti-bluff covenant and must be
investigated immediately.

---

## Documentation

- [`docs/test-coverage.md`](docs/test-coverage.md) — symbol→test ledger.
  Every exported function has at least one row mapping it to a
  unit-test name and (where applicable) a Challenge invariant ID.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — package layout,
  composition with `digital.vasic.concurrency`, design patterns
  (Decorator/Registry/Observer/Facade) applied to each package.
- [`docs/API_REFERENCE.md`](docs/API_REFERENCE.md) — full public API.
- [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md) — operator-facing guide.

---

## Constitutional posture (CONST-047 / CONST-050 / CONST-051)

Recovery is an **equal part** of every consuming project's codebase
(CONST-051(A)). It is **fully decoupled** from any specific consumer:
no project-specific paths, hostnames, or naming schemes leak into
this tree (CONST-051(B)). Its single own-org dependency
(`digital.vasic.concurrency`) is consumed via `replace` directive
against the consuming project's flat layout — no nested own-org
submodule chains (CONST-051(C)).

Verbatim 2026-05-19 operator mandate (preserved per CONST-049 §11.4.17):

> "all existing tests and Challenges do work in anti-bluff manner -
> they MUST confirm that all tested codebase really works as
> expected! We had been in position that all tests do execute with
> success and all Challenges as well, but in reality the most of
> the features does not work and can't be used! This MUST NOT be
> the case and execution of tests and Challenges MUST guarantee
> the quality, the completition and full usability by end users of
> the product!"
