package main

import (
	"ai-business-api/internal/businessai"
	"ai-business-api/internal/customer"
	"ai-business-api/internal/dashboard"
	"ai-business-api/internal/document"
	"ai-business-api/internal/gmail"
	"ai-business-api/internal/imports"
	"ai-business-api/internal/invoice"
	"ai-business-api/internal/notification"
	"ai-business-api/internal/opportunity"
	"ai-business-api/internal/product"
	"ai-business-api/internal/project"
	"ai-business-api/internal/quote"
	"ai-business-api/internal/scope"
	"ai-business-api/internal/settings"
	"ai-business-api/internal/user"
	"ai-business-api/platform/auth"
	"ai-business-api/platform/config"
	"ai-business-api/platform/documentstorage"
	"ai-business-api/platform/googlemail"
	"ai-business-api/platform/httpserver"
	"ai-business-api/platform/llm"
	"ai-business-api/platform/notifymail"
	"ai-business-api/platform/paddleocr"
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("API stopped", "reason", err.Error())
		os.Exit(1)
	}
}
func run(logger *slog.Logger) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, err := pgxpool.New(ctx, c.DatabaseURL)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer db.Close()
	if c.Issuer == "" || c.JWKSURL == "" || c.Origin == "" {
		return errors.New("OIDC_ISSUER, OIDC_JWKS_URL and APP_URL are required")
	}
	storageConfig, err := documentstorage.Read(os.Getenv)
	if err != nil {
		return err
	}
	blobs, err := documentstorage.Open(ctx, storageConfig)
	if err != nil {
		return err
	}
	mailConfig, err := notifymail.Read(os.Getenv)
	if err != nil {
		return err
	}
	gmailConfig, err := googlemail.Read(os.Getenv)
	if err != nil {
		return err
	}
	mailbox := &gmail.Service{DB: db}
	aiConfig, err := llm.Read(os.Getenv)
	if err != nil {
		return err
	}
	if aiConfig.Enabled() {
		mailbox.LLM = llm.New(aiConfig)
		mailbox.AIProvider = aiConfig.Label
		mailbox.AIModel = aiConfig.Model
	}
	summaries := &businessai.Service{DB: db, LLM: mailbox.LLM, Provider: mailbox.AIProvider, Model: mailbox.AIModel}
	if gmailConfig.Enabled() {
		mailbox.Provider = googlemail.New(gmailConfig)
		mailbox.Vault, err = googlemail.NewVault(gmailConfig.Key)
		if err != nil {
			return err
		}
	}
	notifications := &notification.Service{DB: db}
	if mailConfig.Enabled {
		notifications.Mailer = notifymail.Sender{Config: mailConfig}
		notifications.EmailFrom = mailConfig.From
	}
	documents := &document.Service{LLM: mailbox.LLM, AIProvider: mailbox.AIProvider, AIModel: mailbox.AIModel, DB: db, Blobs: blobs, StorageID: storageConfig.ID(), LegacyBlobs: document.LocalStore{Root: storageConfig.LocalPath}}
	if endpoint := os.Getenv("OCR_URL"); endpoint != "" {
		documents.OCR, err = paddleocr.New(endpoint)
		if err != nil {
			return err
		}
	}
	if endpoint := os.Getenv("KHMER_OCR_URL"); endpoint != "" {
		documents.KhmerOCR, err = paddleocr.New(endpoint)
		if err != nil {
			return err
		}
	}
	health := &settings.HealthMonitor{Database: db}
	health.Storage, _ = blobs.(settings.HealthProbe)
	health.OCR, _ = documents.OCR.(settings.HealthProbe)
	health.KhmerOCR, _ = documents.KhmerOCR.(settings.HealthProbe)
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		summaryDone := make(chan struct{})
		go func() { defer close(summaryDone); summaries.Run(workerCtx, logger) }()
		defer func() { <-summaryDone }()
		aiDone := make(chan struct{})
		go func() { defer close(aiDone); mailbox.RunAssistance(workerCtx, logger) }()
		defer func() { <-aiDone }()
		gmailDone := make(chan struct{})
		go func() { defer close(gmailDone); mailbox.RunSync(workerCtx, logger) }()
		defer func() { <-gmailDone }()
		extractionDone := make(chan struct{})
		go func() { defer close(extractionDone); documents.RunExtraction(workerCtx, logger) }()
		defer func() { <-extractionDone }()
		ocrDone := make(chan struct{})
		go func() { defer close(ocrDone); documents.RunOCR(workerCtx, logger) }()
		defer func() { <-ocrDone }()
		emailDone := make(chan struct{})
		go func() { defer close(emailDone); notifications.RunEmail(workerCtx, logger) }()
		notifications.Run(workerCtx, logger)
		<-emailDone
	}()
	defer func() { cancelWorker(); <-workerDone }()
	service := &user.Service{Store: &user.Repository{DB: db}}
	verifier := auth.New(ctx, c.Issuer, "ai-business-api", c.JWKSURL)
	server := &http.Server{Addr: c.Address, Handler: httpserver.New(db, logger, httpserver.Options{Imports: &imports.Service{DB: db}, BusinessAI: summaries, Gmail: mailbox, Settings: &settings.Service{Health: health, Integrations: settings.IntegrationConfig{StorageBackend: storageConfig.Backend, AI: aiConfig.Enabled(), Gmail: gmailConfig.Enabled(), Email: mailConfig.Enabled, OCR: documents.OCR != nil, KhmerOCR: documents.KhmerOCR != nil}, DB: db, Blobs: blobs, StorageID: storageConfig.ID(), LegacyBlobs: document.LocalStore{Root: storageConfig.LocalPath}}, Notifications: notifications, Dashboard: &dashboard.Service{DB: db}, Documents: documents, Scopes: &scope.Service{DB: db}, Invoices: &invoice.Service{DB: db}, Projects: &project.Service{Store: &project.Repository{DB: db}}, Products: &product.Service{DB: db}, Quotes: &quote.Service{DB: db}, Opportunities: &opportunity.Service{Store: &opportunity.Repository{DB: db}}, Customers: &customer.Service{Store: &customer.Repository{DB: db}}, Service: service, Verifier: verifier, Origin: c.Origin}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	failed := make(chan error, 1)
	go func() { logger.Info("API listening", "address", c.Address); failed <- server.ListenAndServe() }()
	select {
	case err := <-failed:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP server failed")
		}
		return nil
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}
