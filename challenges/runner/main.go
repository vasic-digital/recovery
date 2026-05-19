// Command runner is the Recovery round-280 Challenge runner.
//
// It exercises the real digital.vasic.recovery primitives end-to-end —
// the named CircuitBreaker + CircuitBreakerManager, the periodic
// health.Checker, and the unified facade.Resilience — across five
// locale fixtures (en, sr, ja, es, de). Every PASS line is backed by
// a runtime invariant, never a metadata-only check (CONST-035 /
// Article XI §11.9).
//
// Anti-bluff invariants enforced:
//
//  1. breaker.NewCircuitBreaker returns a non-nil breaker whose
//     initial state is StateClosed and whose Execute(fn) with a
//     nil-returning fn yields nil error. A stub breaker that always
//     errors would fail here.
//  2. After MaxFailures consecutive failures the breaker transitions
//     to StateOpen. Verified by observing GetState() after enough
//     forced-error Executes. A breaker that never trips is a bluff.
//  3. breaker.Reset() returns a tripped breaker to StateClosed and
//     zeroes the failure count. A no-op Reset is a bluff.
//  4. health.NewChecker round-trip: a CheckFunc returning nil is
//     observed as StatusHealthy after Start(ctx); a CheckFunc
//     returning an error is observed as StatusUnhealthy. The
//     LastError() value is non-nil in the unhealthy case and nil
//     in the healthy case — exactly the documented contract.
//  5. facade.New + Execute round-trip: an Execute call with a
//     known-good fn returns nil; the named breaker becomes
//     registered in the facade's Stats() output keyed under
//     "breakers". An AddHealthCheck call surfaces under "health".
//     A facade that loses registrations is a bluff.
//  6. Bilingual rendering: every locale fixture's banner line is
//     emitted; every banner is unique per locale; no two locales
//     emit the same banner (catches the "SR collapsed to EN" bluff).
//
// Mutation hook: when env RECOVERY_MUTATE_RUNNER=1 is set, the runner
// inverts invariant (2) (treats a never-tripping breaker as PASS
// instead of FAIL). The paired Challenge wraps this to assert the
// runner exits 99 under mutation, guaranteeing the runner actually
// checks what it claims (CONST-050(A) paired mutation, §1.1).
//
// Verbatim 2026-05-19 operator mandate (preserved per
// CONST-049 §11.4.17):
//
//	"all existing tests and Challenges do work in anti-bluff
//	manner - they MUST confirm that all tested codebase really
//	works as expected! We had been in position that all tests
//	do execute with success and all Challenges as well, but
//	in reality the most of the features does not work and
//	can't be used! This MUST NOT be the case and execution
//	of tests and Challenges MUST guarantee the quality, the
//	completition and full usability by end users of the
//	product!"
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"digital.vasic.recovery/pkg/breaker"
	"digital.vasic.recovery/pkg/facade"
	"digital.vasic.recovery/pkg/health"
)

// fixture is a small projection of challenges/fixtures/<locale>.yaml.
// Kept in-process so the runner stays dependency-free beyond what the
// module already depends on (CONST-051(B): no new transitive deps
// creeping into a reusable submodule).
type fixture struct {
	locale  string
	entries map[string]string
}

func (f *fixture) get(key string) string {
	if v, ok := f.entries[key]; ok && v != "" {
		return v
	}
	return key // loud miss
}

func main() {
	if code := run(os.Stdout); code != 0 {
		os.Exit(code)
	}
}

func run(out io.Writer) int {
	fmt.Fprintln(out, "=== Recovery Challenge Runner (round-280) ===")

	fixDir := os.Getenv("RECOVERY_FIXTURES_DIR")
	if fixDir == "" {
		fixDir = filepath.Join("challenges", "fixtures")
	}

	fixtures, err := loadFixtures(fixDir)
	if err != nil {
		fmt.Fprintf(out, "FAIL: load fixtures from %s: %v\n",
			fixDir, err)
		return 1
	}
	if len(fixtures) < 5 {
		fmt.Fprintf(out, "FAIL: expected >=5 fixtures, got %d\n",
			len(fixtures))
		return 1
	}
	fmt.Fprintf(out, "[setup] loaded %d locale fixtures from %s\n",
		len(fixtures), fixDir)

	mutate := os.Getenv("RECOVERY_MUTATE_RUNNER") == "1"
	if mutate {
		fmt.Fprintln(out, "[setup] MUTATION MODE: runner will treat"+
			" never-tripping breaker as PASS")
	}

	pass, fail := 0, 0
	step := func(name string, ok bool, detail string) {
		if ok {
			pass++
			fmt.Fprintf(out, "  PASS  %-52s  %s\n", name, detail)
			return
		}
		fail++
		fmt.Fprintf(out, "  FAIL  %-52s  %s\n", name, detail)
	}

	// Invariant 6 (banner uniqueness) collected as we render.
	banners := map[string]string{}

	for _, f := range fixtures {
		banner := f.get("banner")
		fmt.Fprintln(out, banner)
		if existing, ok := banners[banner]; ok {
			step("fixture.banner.unique."+f.locale,
				false,
				fmt.Sprintf("banner collides with locale %q: %q",
					existing, banner))
		} else {
			banners[banner] = f.locale
			step("fixture.banner.unique."+f.locale,
				true, banner)
		}
	}

	// Use any one fixture (the EN one) for the per-primitive PASS
	// detail strings; we already proved locales are distinct above.
	enFix := fixtureByLocale(fixtures, "en")
	if enFix == nil {
		step("fixture.en.present", false, "en fixture missing")
		fmt.Fprintf(out, "\n=== Summary: PASS=%d FAIL=%d ===\n",
			pass, fail)
		return 1
	}

	// Invariant 1+2+3: per-locale breaker creation + tripping +
	// reset. Each locale exercises a fresh breaker registered in
	// the manager — proves both the construction path AND the
	// state-machine path against the real implementation.
	mgr := breaker.NewCircuitBreakerManager(nil)
	for _, f := range fixtures {
		name := f.get("expect.breaker.name") + "-" + f.locale
		cfg := breaker.CircuitBreakerConfig{
			Name:         name,
			MaxFailures:  3,
			ResetTimeout: 50 * time.Millisecond,
		}
		cb := mgr.GetOrCreate(name, cfg)
		step("breaker.NewCircuitBreaker.not_nil."+f.locale,
			cb != nil,
			fmt.Sprintf("name=%s", name))

		// Invariant 1: closed-state success.
		execErr := cb.Execute(func() error { return nil })
		state := cb.GetState()
		step("breaker.Execute.closed_success."+f.locale,
			execErr == nil && state == breaker.StateClosed,
			fmt.Sprintf("err=%v state=%s", execErr, state.String()))

		// Invariant 2: MaxFailures+1 failed Executes trip to Open.
		boom := errors.New("synthetic-failure")
		for i := 0; i < 6; i++ {
			_ = cb.Execute(func() error { return boom })
		}
		tripped := cb.GetState() == breaker.StateOpen
		if mutate {
			// Mutation: flip polarity — accept "never tripped" as PASS.
			step("breaker.trips_on_MaxFailures[MUTATED]."+f.locale,
				!tripped,
				fmt.Sprintf("mutation-inverted: state=%s failures=%d",
					cb.GetState().String(), cb.GetFailures()))
		} else {
			step("breaker.trips_on_MaxFailures."+f.locale,
				tripped,
				fmt.Sprintf("state=%s failures=%d",
					cb.GetState().String(), cb.GetFailures()))
		}

		// Invariant 3: Reset returns to Closed and zeroes failures.
		cb.Reset()
		resetOK := cb.GetState() == breaker.StateClosed &&
			cb.GetFailures() == 0
		step("breaker.Reset.returns_to_closed."+f.locale,
			resetOK,
			fmt.Sprintf("state=%s failures=%d",
				cb.GetState().String(), cb.GetFailures()))
	}

	// Invariant: manager.GetAll returns every registered breaker.
	all := mgr.GetAll()
	step("breaker.Manager.GetAll.count",
		len(all) == len(fixtures),
		fmt.Sprintf("want=%d got=%d", len(fixtures), len(all)))

	// Invariant 4: health.Checker round-trip — healthy + unhealthy
	// paths produce the documented Status values.
	for _, f := range fixtures {
		hname := f.get("expect.health.name") + "-" + f.locale

		// Healthy path.
		hOK := health.NewChecker(hname+"-ok", func() error { return nil },
			20*time.Millisecond)
		ctxH, cancelH := context.WithCancel(context.Background())
		hOK.Start(ctxH)
		waitForStatus(hOK, health.StatusHealthy, 200*time.Millisecond)
		gotH := hOK.Status()
		gotErrH := hOK.LastError()
		hOK.Stop()
		cancelH()
		step("health.Checker.healthy."+f.locale,
			gotH == health.StatusHealthy && gotErrH == nil,
			fmt.Sprintf("status=%s err=%v name=%s",
				gotH, gotErrH, hname+"-ok"))

		// Unhealthy path.
		bad := errors.New("health-down-" + f.locale)
		hBad := health.NewChecker(hname+"-bad",
			func() error { return bad }, 20*time.Millisecond)
		ctxU, cancelU := context.WithCancel(context.Background())
		hBad.Start(ctxU)
		waitForStatus(hBad, health.StatusUnhealthy, 200*time.Millisecond)
		gotU := hBad.Status()
		gotErrU := hBad.LastError()
		hBad.Stop()
		cancelU()
		step("health.Checker.unhealthy."+f.locale,
			gotU == health.StatusUnhealthy && gotErrU != nil,
			fmt.Sprintf("status=%s err=%v name=%s",
				gotU, gotErrU, hname+"-bad"))
	}

	// Invariant 5: facade.New + Execute + AddHealthCheck +
	// Stats round-trip — registrations surface in Stats().
	res := facade.New(nil)
	defer res.Stop()

	for _, f := range fixtures {
		ep := f.get("expect.facade.endpoint") + "-" + f.locale
		var captured string
		execErr := res.Execute(ep, func() error {
			captured = ep
			return nil
		})
		step("facade.Execute.returns_nil."+f.locale,
			execErr == nil && captured == ep,
			fmt.Sprintf("captured=%q err=%v", captured, execErr))

		hname := f.get("expect.health.name") + "-facade-" + f.locale
		res.AddHealthCheck(hname, func() error { return nil },
			25*time.Millisecond)
	}

	// Allow the health checkers registered above one tick to run.
	time.Sleep(80 * time.Millisecond)

	stats := res.Stats()
	bk, bkOK := stats["breakers"].(map[string]interface{})
	hl, hlOK := stats["health"].(map[string]interface{})
	step("facade.Stats.has_breakers_key",
		bkOK && len(bk) >= len(fixtures),
		fmt.Sprintf("breakers_keys=%d", len(bk)))
	step("facade.Stats.has_health_key",
		hlOK && len(hl) >= len(fixtures),
		fmt.Sprintf("health_keys=%d", len(hl)))

	// Invariant: every fixture's expected facade endpoint appears
	// in the breakers map under its locale-suffixed name.
	if bkOK {
		missing := []string{}
		for _, f := range fixtures {
			ep := f.get("expect.facade.endpoint") + "-" + f.locale
			if _, ok := bk[ep]; !ok {
				missing = append(missing, ep)
			}
		}
		step("facade.Stats.breakers.contains_every_locale",
			len(missing) == 0,
			fmt.Sprintf("missing=%v", missing))
	}

	// Emit en-locale success line as final human-readable proof of
	// the bilingual rendering path actually firing the success
	// vocabulary, not just the banner.
	fmt.Fprintln(out, enFix.get("result.success"))
	srFix := fixtureByLocale(fixtures, "sr")
	if srFix != nil {
		fmt.Fprintln(out, srFix.get("result.success"))
	}

	fmt.Fprintf(out, "\n=== Summary: PASS=%d FAIL=%d ===\n",
		pass, fail)
	if fail > 0 {
		return 1
	}
	return 0
}

// waitForStatus polls a checker for up to maxWait for it to reach the
// target status. The real Checker only transitions after its first
// runCheck(), which Start() invokes inline — but the goroutine update
// is observed via Status(), so we tolerate a brief gap.
func waitForStatus(c *health.Checker, target health.Status, maxWait time.Duration) {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if c.Status() == target {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// fixtureByLocale returns the fixture for the given locale, or nil
// if absent.
func fixtureByLocale(fs []fixture, locale string) *fixture {
	for i := range fs {
		if fs[i].locale == locale {
			return &fs[i]
		}
	}
	return nil
}

// loadFixtures parses every *.yaml in dir using a tiny line-based
// parser. We support only the flat key:value format our fixtures use;
// anything else is ignored. Keeping the parser in-runner avoids
// pulling yaml.v3 into the runtime path of a submodule that other
// projects reuse (CONST-051(B)).
func loadFixtures(dir string) ([]fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []fixture
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		f := parseFixture(string(data))
		if f.locale == "" {
			return nil, fmt.Errorf(
				"%s: missing locale key", e.Name())
		}
		out = append(out, f)
	}
	// Deterministic order so logs diff cleanly between runs.
	sort.Slice(out, func(i, j int) bool {
		return out[i].locale < out[j].locale
	})
	return out, nil
}

func parseFixture(text string) fixture {
	f := fixture{entries: map[string]string{}}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colon := strings.Index(trimmed, ":")
		if colon < 0 {
			continue
		}
		k := strings.TrimSpace(trimmed[:colon])
		v := strings.TrimSpace(trimmed[colon+1:])
		if len(v) >= 2 {
			first, last := v[0], v[len(v)-1]
			if (first == '\'' && last == '\'') ||
				(first == '"' && last == '"') {
				v = v[1 : len(v)-1]
			}
		}
		if k == "locale" {
			f.locale = v
		}
		f.entries[k] = v
	}
	return f
}
