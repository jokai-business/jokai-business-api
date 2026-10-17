package main

import (
	"strings"
	"testing"
)

func TestDatabaseURL(t *testing.T) {
	raw := "postgres://tester:fixture@localhost:5432/postgres?sslmode=disable"
	target, e := databaseURL(raw, "jokai_it_test_1")
	if e != nil || !strings.Contains(target, "/jokai_it_test_1?sslmode=disable") {
		t.Fatal("database not replaced", e)
	}
	for _, bad := range []string{"", "postgres://localhost/live", "postgres://localhost/postgres?dbname=live", "postgres://localhost/postgres?host=other", "postgres://localhost/postgres?service=live", "file:///postgres", "postgres://localhost/postgres#fragment"} {
		if _, e := databaseURL(bad, "test"); e == nil {
			t.Fatal("unsafe target accepted")
		}
	}
}
func TestRequiredSuitesCannotSkip(t *testing.T) {
	states := map[string]string{}
	if requiredPassed(states) == nil {
		t.Fatal("missing tests accepted")
	}
	for _, s := range suites {
		states[key("ai-business-api/internal/"+s.pkg, s.test)] = "pass"
	}
	if e := requiredPassed(states); e != nil {
		t.Fatal(e)
	}
	for _, s := range suites {
		k := key("ai-business-api/internal/"+s.pkg, s.test)
		for _, state := range []string{"skip", "fail", "run"} {
			states[k] = state
			if requiredPassed(states) == nil {
				t.Fatal("incomplete suite accepted", s.test, state)
			}
		}
		states[k] = "pass"
	}
	states["ai-business-api/internal/future/TestFutureIntegration"] = "skip"
	if requiredPassed(states) == nil {
		t.Fatal("new skipped integration suite accepted")
	}

}
