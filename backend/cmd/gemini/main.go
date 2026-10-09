// Command gemini — единый бинарь СРК Gemini.
//
//	gemini serve      запуск HTTP-сервера головы (по умолчанию)
//	gemini migrate    применить миграции своей БД метаданных и выйти
//	gemini dump       выполнить дамп (исполняется внутри k8s Job)
//	gemini restore    выполнить restore (исполняется внутри k8s Job)
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/api"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/db"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/jobrunner"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/k8s"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/notify"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/operator"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/peer"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/secretcrypto"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/vault"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "dump", "restore":
		if err := jobrunner.Run(context.Background(), cmd); err != nil {
			log.Fatalf("%s: %v", cmd, err)
		}
		return
	case "migrate":
		runMigrate()
		return
	case "serve":
		runServe()
	default:
		log.Fatalf("unknown command %q (want serve|migrate|dump|restore)", cmd)
	}
}

func runMigrate() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.OwnDatabaseDSN)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations complete")
}

func runServe() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	log.Printf("gemini: starting as %s head", cfg.Role)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.Connect(ctx, cfg.OwnDatabaseDSN)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	// Миграции сознательно НЕ применяются на старте serve — их накатывает
	// отдельный `gemini migrate` (Helm hook Job / compose-сервис `migrate`).
	if err := db.EnsureMigrated(ctx, pool); err != nil {
		log.Fatalf("db: %v (run `gemini migrate` first)", err)
	}

	box, err := secretcrypto.New(cfg.SecretEncryptionKey)
	if err != nil {
		log.Fatalf("secretcrypto: %v", err)
	}
	st := store.New(pool, box)

	// Ключ головы (Ed25519): Source подписывает им дампы, Target'ы проверяют
	// запиненным публичным ключом. Генерится один раз, дальше живёт в БД.
	if pub, err := st.EnsureHeadIdentity(ctx); err != nil {
		log.Fatalf("head identity: %v", err)
	} else {
		log.Printf("gemini: head public key %s", base64.StdEncoding.EncodeToString(pub))
	}

	verifier, err := auth.NewVerifier(ctx, cfg.OIDC)
	if err != nil {
		log.Fatalf("oidc: %v", err)
	}

	var vc *vault.Client
	if cfg.Vault.Enabled {
		if vc, err = vault.New(cfg.Vault); err != nil {
			log.Fatalf("vault: %v", err)
		}
	}

	// Бакет из env (Helm values.s3) — встроенный; остальные добавляются из UI.
	// Дампы заливаются во все включённые (fan-out).
	var builtIn *model.S3Bucket
	if cfg.S3.Bucket != "" && cfg.S3.AccessKey != "" {
		builtIn = &model.S3Bucket{
			Name: "helm", Endpoint: cfg.S3.Endpoint, Region: cfg.S3.Region, Bucket: cfg.S3.Bucket,
			AccessKeyID: cfg.S3.AccessKey, SecretAccessKey: cfg.S3.SecretKey,
			PathStyle: cfg.S3.UsePathStyle, UseSSL: cfg.S3.UseSSL,
		}
	} else {
		log.Printf("gemini: S3 not configured in env — only buckets added in UI will be used")
	}
	sc := storage.NewPool(builtIn, st.EnabledS3Buckets)

	var kc *k8s.Client
	if kc, err = k8s.New(cfg.K8sNamespace); err != nil {
		log.Printf("gemini: kubernetes client unavailable (%v) — job orchestration disabled", err)
		kc = nil
	} else if err := kc.ResolveOwner(ctx, cfg.JobOwnerDeployment); err != nil {
		log.Printf("gemini: %v — job'ы без ownerReference/imagePullSecrets", err)
	}

	nt := notify.New(cfg.Notify)
	peerClient := peer.NewClient(cfg.PeerRetry, string(cfg.Role))
	peerVerifier := peer.NewVerifier(cfg.PeerRetry.ClockSkew, cfg.PeerRole(), st.ActivePairingSecrets)

	op := operator.New(cfg, st, kc, vc, sc, nt, peerClient)

	router := api.NewRouter(api.Deps{
		Cfg:          cfg,
		Store:        st,
		Operator:     op,
		Verifier:     verifier,
		PeerVerifier: peerVerifier,
		Peer:         peerClient,
		Vault:        vc,
		Storage:      sc,
		Ready: func() error {
			c, cc := context.WithTimeout(context.Background(), 5*time.Second)
			defer cc()
			if err := pool.Ping(c); err != nil {
				return fmt.Errorf("db: %w", err)
			}
			if err := sc.Ping(c); err != nil {
				return err
			}
			if vc != nil {
				if err := vc.Ping(c); err != nil {
					return err
				}
			}
			return nil
		},
	})

	// Фоновые компоненты
	go op.Watch(ctx)
	go op.RunTemplateReconciler(ctx) // CronJob-шаблоны + секреты кред (dump/restore)
	if cfg.IsTarget() {
		go op.RunPoller(ctx)
	}
	if cfg.IsSource() {
		go op.RunSignatureBackfill(ctx)
	}

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: router,
		// ReadHeaderTimeout спасал только от медленных заголовков — медленное тело
		// держало соединение бесконечно. WriteTimeout с запасом: самый долгий
		// хендлер — discovery (20с внутренний ctx) и pairing/init (15с к пиру).
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		log.Printf("gemini: listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("gemini: received %v, shutting down", sig)

	shutdownCtx, sc2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer sc2()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	cancel()
	log.Println("gemini: bye")
}
