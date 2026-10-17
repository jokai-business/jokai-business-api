// bootstrap provisions the first organization administrator from an existing
// Keycloak user subject. It never creates passwords or grants access on login.
package main

import (
	"context"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Bootstrap failed; verify inputs and database state")
		os.Exit(1)
	}
	slog.Info("Organization administrator provisioned")
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	subject, email, name, org := os.Getenv("BOOTSTRAP_SUBJECT"), os.Getenv("BOOTSTRAP_EMAIL"), os.Getenv("BOOTSTRAP_NAME"), os.Getenv("BOOTSTRAP_ORGANIZATION")
	if subject == "" || email == "" || name == "" || org == "" {
		return pgx.ErrNoRows
	}
	db, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close(ctx)
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var oid, uid string
	if err = tx.QueryRow(ctx, "INSERT INTO organizations(name) VALUES($1) RETURNING id", org).Scan(&oid); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, "INSERT INTO users(organization_id,subject,email,name) VALUES($1,$2,$3,$4) RETURNING id", oid, subject, email, name).Scan(&uid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO user_roles(organization_id,user_id,role_key) VALUES($1,$2,'organization_admin')", oid, uid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_logs(organization_id,actor_id,action,resource_id,request_id) VALUES($1,$2,'organization.bootstrap',$1,'bootstrap')", oid, uid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
