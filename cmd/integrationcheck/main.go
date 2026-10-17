// integrationcheck runs every backend test with disposable databases, and rejects skipped integration suites.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
)

type suite struct{ env, pkg, test string }

var suites = []suite{
	{"NOTIFICATION_TEST_DATABASE_URL", "notification", "TestEmailQueueIntegration"},
	{"DOCUMENT_TEST_DATABASE_URL", "document", "TestOCRIntegration"},
	{"EXTRACTION_TEST_DATABASE_URL", "document", "TestExtractionIntegration"},
	{"GMAIL_TEST_DATABASE_URL", "gmail", "TestConnectionIntegration"},
	{"AI_TEST_DATABASE_URL", "gmail", "TestAssistanceIntegration"},
	{"SUMMARY_TEST_DATABASE_URL", "businessai", "TestSummaryIntegration"},
	{"QUESTION_TEST_DATABASE_URL", "businessai", "TestQuestionIntegration"},
	{"IMPORT_TEST_DATABASE_URL", "imports", "TestImportIntegration"},
}

type event struct{ Action, Package, Test, Output string }

func key(pkg, test string) string { return pkg + "/" + test }
func requiredPassed(events map[string]string) error {
	for name, status := range events {
		if status == "skip" && strings.Contains(name, "Integration") {
			return fmt.Errorf("integration test was skipped: %s", name)
		}
	}
	for _, s := range suites {
		if events[key("ai-business-api/internal/"+s.pkg, s.test)] != "pass" {
			return fmt.Errorf("required integration suite did not pass: %s/%s", s.pkg, s.test)
		}
	}
	return nil
}
func databaseURL(raw, name string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Fragment != "" {
		return "", errors.New("use a PostgreSQL URL for the disposable server")
	}
	// Prevent query parameters overriding the database name selected by the runner.
	for _, v := range []string{"dbname", "database", "host", "hostaddr", "port", "service", "servicefile"} {
		if u.Query().Has(v) {
			return "", errors.New("connection routing overrides are not allowed")
		}
	}
	if u.Path != "/postgres" {
		return "", errors.New("disposable server control database must be postgres")
	}
	u.Path = "/" + name
	u.RawPath = ""
	return u.String(), nil
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context) (result error) {
	if os.Getenv("INTEGRATION_DISPOSABLE_SERVER") != "yes" {
		return errors.New("set INTEGRATION_DISPOSABLE_SERVER=yes for a dedicated empty test server")
	}
	raw := os.Getenv("INTEGRATION_ADMIN_URL")
	if _, e := databaseURL(raw, "postgres"); e != nil {
		return e
	}
	// Never inherit application database targets; only the dedicated admin URL is used.
	config, e := pgx.ParseConfig(raw)
	if e != nil {
		return errors.New("invalid disposable server configuration")
	}
	config.ConnectTimeout = 2 * time.Second
	startup, cancelStartup := context.WithTimeout(ctx, 30*time.Second)
	defer cancelStartup()
	var conn *pgx.Conn
	for {
		conn, e = pgx.ConnectConfig(startup, config)
		if e == nil {
			break
		}
		select {
		case <-startup.Done():
			return errors.New("cannot connect to disposable integration server within 30 seconds")
		case <-time.After(500 * time.Millisecond):
		}
	}
	defer conn.Close(context.Background())
	var databases, tables int
	e = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_database WHERE NOT datistemplate AND datname<>'postgres'),(SELECT count(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema'))`).Scan(&databases, &tables)
	if e != nil || databases != 0 || tables != 0 {
		return errors.New("refusing integration setup: server must contain only an empty postgres database")
	}
	var random [8]byte
	if _, e = rand.Read(random[:]); e != nil {
		return errors.New("cannot create integration run identity")
	}
	prefix := "jokai_it_" + hex.EncodeToString(random[:]) + "_"
	created := []string{}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, name := range created {
			// Only names successfully created by this invocation are eligible for cleanup.
			if _, err := conn.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
				result = errors.New("integration cleanup failed; remove this run's disposable server")
			}
		}
	}()
	env := []string{}
	for _, v := range os.Environ() {
		name, _, _ := strings.Cut(v, "=")
		if name == "DATABASE_URL" || name == "INTEGRATION_ADMIN_URL" || strings.HasSuffix(name, "_TEST_DATABASE_URL") {
			continue
		}
		env = append(env, v)
	}
	for i, s := range suites {
		name := fmt.Sprintf("%s%d", prefix, i)
		if _, e = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); e != nil {
			return errors.New("cannot create disposable integration database")
		}
		created = append(created, name)
		target, _ := databaseURL(raw, name)
		env = append(env, s.env+"="+target)
	}
	timeout, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(timeout, "go", "test", "-json", "-count=1", "-timeout=5m", "./...")
	cmd.Env = env
	cmd.Stderr = os.Stderr
	output, e := cmd.StdoutPipe()
	if e != nil {
		return errors.New("cannot capture test results")
	}
	if e = cmd.Start(); e != nil {
		return errors.New("cannot start Go tests")
	}
	states := map[string]string{}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 65536), 4<<20)
	for scanner.Scan() {
		var v event
		if json.Unmarshal(scanner.Bytes(), &v) != nil {
			continue
		}
		if v.Test != "" && (v.Action == "pass" || v.Action == "fail" || v.Action == "skip") {
			states[key(v.Package, v.Test)] = v.Action
		}
		// Report identities/status only; failure diagnostics can contain fixture records or DSNs.
		if (v.Action == "pass" || v.Action == "fail" || v.Action == "skip") && (v.Test == "" || strings.Contains(v.Test, "Integration")) {
			fmt.Printf("%s %s %s\n", v.Action, v.Package, v.Test)
		}
	}
	if scanner.Err() != nil {
		cancel()
	}
	e = cmd.Wait()
	if scanner.Err() != nil {
		return errors.New("could not read complete test results")
	}
	if e != nil {
		return errors.New("backend tests failed; rerun the failing test against a disposable fixture for detailed diagnostics")
	}
	if e = requiredPassed(states); e != nil {
		return e
	}
	fmt.Printf("All %d required database integration suites passed; all backend tests completed.\n", len(suites))
	return nil
}
