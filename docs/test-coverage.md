# Recovery — Symbol → Test Coverage Ledger

Maps every exported symbol in `digital.vasic.recovery` to the test
case(s) that exercise it and (where applicable) to the Challenge-
runner invariant that confirms the same behaviour end-to-end with
captured runtime evidence (CONST-035 / Article XI §11.9).

A row without a Challenge invariant is acceptable ONLY for symbols
that are infrastructure for the public API (loggers, internal
mappers) — every user-facing primitive MUST have at least one
captured-evidence path.

---

## `pkg/breaker`

| Symbol | Unit test | Challenge invariant |
|---|---|---|
| `CircuitState` (enum) | `TestCircuitState_String` (breaker_test.go) | — |
| `CircuitState.String()` | `TestCircuitState_String` | — |
| `CircuitBreakerConfig` (struct) | `TestNewCircuitBreaker_DefaultsApplied` | runner invariant 1 |
| `NewCircuitBreaker(config)` | `TestNewCircuitBreaker_*` (breaker_test.go, breaker_edge_test.go) | runner invariant 1 (per-locale) |
| `(*CircuitBreaker).SetStateChangeCallback(cb)` | `TestCircuitBreaker_StateChangeCallback` | — |
| `(*CircuitBreaker).Execute(fn)` | `TestCircuitBreaker_Execute_*` (breaker_test.go), `TestCircuitBreaker_TripsOnMaxFailures` (breaker_edge_test.go) | runner invariants 1, 2 (per-locale) |
| `(*CircuitBreaker).GetState()` | `TestCircuitBreaker_GetState_*` | runner invariants 1, 2, 3 |
| `(*CircuitBreaker).GetFailures()` | `TestCircuitBreaker_GetFailures` | runner invariant 3 |
| `(*CircuitBreaker).GetStats()` | `TestCircuitBreaker_GetStats` (breaker_coverage_test.go) | — |
| `(*CircuitBreaker).Reset()` | `TestCircuitBreaker_Reset` (breaker_edge_test.go) | runner invariant 3 (per-locale) |
| `NewCircuitBreakerManager(logger)` | `TestNewCircuitBreakerManager` | runner GetAll cardinality assertion |
| `(*CircuitBreakerManager).GetOrCreate(name, cfg)` | `TestCircuitBreakerManager_GetOrCreate` | runner invariants 1-3 (registry path) |
| `(*CircuitBreakerManager).Get(name)` | `TestCircuitBreakerManager_Get` | — |
| `(*CircuitBreakerManager).GetAll()` | `TestCircuitBreakerManager_GetAll` (breaker_edge2_test.go) | runner `breaker.Manager.GetAll.count` invariant |
| `(*CircuitBreakerManager).GetStats()` | `TestCircuitBreakerManager_GetStats` | facade Stats invariant |
| `(*CircuitBreakerManager).Reset()` | `TestCircuitBreakerManager_Reset` | — |
| `Logger` (interface) | exercised via callback test | — |
| `mapBreakState(s)` (internal) | exercised transitively via `GetState` | — |

## `pkg/health`

| Symbol | Unit test | Challenge invariant |
|---|---|---|
| `Status` (type) | `TestStatus_Values` (health_test.go) | — |
| `CheckFunc` (type) | exercised by every Checker test | runner invariant 4 |
| `NewChecker(name, fn, interval)` | `TestNewChecker_*` (health_test.go, health_edge_test.go) | runner invariant 4 (per-locale healthy + unhealthy) |
| `(*Checker).SetLogger(l)` | `TestChecker_SetLogger` | — |
| `(*Checker).Start(ctx)` | `TestChecker_Start_*` | runner invariant 4 |
| `(*Checker).Stop()` | `TestChecker_Stop` | runner invariant 4 (cleanup leg) |
| `(*Checker).Status()` | `TestChecker_StatusTransitions` (health_edge_test.go) | runner invariant 4 |
| `(*Checker).LastError()` | `TestChecker_LastError` (health_coverage_test.go) | runner invariant 4 (unhealthy leg asserts non-nil) |
| `(*Checker).LastCheck()` | `TestChecker_LastCheck` | facade Stats invariant |
| `(*Checker).Name()` | `TestChecker_Name` | — |
| `Logger` (interface) | exercised via SetLogger test | — |

## `pkg/facade`

| Symbol | Unit test | Challenge invariant |
|---|---|---|
| `Resilience` (struct) | `TestNew_*` (facade_test.go) | runner invariant 5 |
| `New(logger)` | `TestNew_DefaultLogger`, `TestNew_WithLogger` | runner invariant 5 (per-locale Execute path) |
| `(*Resilience).GetOrCreateBreaker(name, cfg)` | `TestResilience_GetOrCreateBreaker` | runner invariant 5 via Execute |
| `(*Resilience).AddHealthCheck(name, fn, interval)` | `TestResilience_AddHealthCheck_*` (facade_edge_test.go) | runner invariant 5 (`facade.Stats.has_health_key`) |
| `(*Resilience).Execute(name, fn)` | `TestResilience_Execute_*` (facade_coverage_test.go) | runner invariant 5 (per-locale captured payload + nil err) |
| `(*Resilience).Stats()` | `TestResilience_Stats` | runner `facade.Stats.*` invariants (3 PASS lines) |
| `(*Resilience).Stop()` | `TestResilience_Stop` | runner defer-Stop cleanup leg |

---

## Test-type matrix

Per CONST-050(B), every applicable test type is exercised:

- **Unit** — `go test ./...` (mocks allowed only here).
- **Integration** — `pkg/facade` tests compose `pkg/breaker` +
  `pkg/health` against real implementations, exercising the
  ticker-driven goroutine path with real `time.Ticker`. No mocks
  beyond pure-test loggers.
- **E2E / Challenge** — `challenges/runner/main.go` exercises the
  real primitives across 5 locale fixtures and is wrapped by
  `challenges/recovery_describe_challenge.sh` for the paired-
  mutation guard.
- **Stress / scaling / chaos / DDoS / UI / UX** — exercised by the
  pre-existing scripts in `challenges/scripts/` (chaos failure
  injection, ddos health flood, scaling horizontal, stress
  sustained load, ui terminal interaction, ux end-to-end flow,
  host no-auto-suspend, no-suspend calls).
- **Paired meta-test mutation (§1.1)** — `recovery_describe_challenge.sh
  mutate` plants a polarity flip and asserts the runner FAILs.
  Exit 99 = mutation detected (assertions are real, not bluffs).
- **Race detector** — every `go test` invocation runs with `-race`.

A row without captured Challenge evidence is a §11.4 PASS-bluff and
must be corrected (either by adding the runner invariant or by
documenting why the symbol is genuinely internal-only). The Challenge
runner itself emits the row count in its summary line: any change to
the public API surface that does not add a corresponding PASS row is
caught by the ledger drift gate.
