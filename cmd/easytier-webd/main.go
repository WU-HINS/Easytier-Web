// Command easytier-webd is the rewritten EasyTier control plane.
//
// The database is configured exclusively through the environment:
//
//	EASYTIER_DB_DRIVER=sqlite|postgres
//	EASYTIER_DB_DSN=/var/lib/easytier/et.db | postgres://user@host/db?sslmode=disable
//
// Everything else is a flag.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/httpapi"
	"github.com/easytier/easytier-console/internal/logging"
	"github.com/easytier/easytier-console/internal/oidc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
	"github.com/easytier/easytier-console/internal/wire"
)

type options struct {
	listenRPC       string
	listenWS        string
	listenHTTP      string
	webDir          string
	secureCookies   bool
	allowAutoCreate bool
	setupToken      string
	heartbeatMin    time.Duration
	heartbeatMax    time.Duration
}

// rpcPort reads the port out of the control-plane listen address, falling back
// to zero when the address has no port (the enrolment guide then omits it).
func rpcPort(listen string) int {
	_, portText, err := net.SplitHostPort(listen)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return 0
	}
	return port
}

func main() {
	if err := run(); err != nil {
		log.Printf("easytier-webd: %v", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.listenRPC, "config-server", "0.0.0.0:22020", "control-plane RPC listener (tcp framing)")
	flag.StringVar(&opts.listenWS, "config-server-ws", "", "optional WebSocket control-plane listener")
	flag.StringVar(&opts.listenHTTP, "http", "0.0.0.0:11211", "console REST listener")
	flag.StringVar(&opts.webDir, "web-dir", "", "serve the built client from this directory")
	flag.BoolVar(&opts.secureCookies, "secure-cookies", true, "mark the session cookie Secure (disable only for plain HTTP dev)")
	flag.BoolVar(&opts.allowAutoCreate, "allow-auto-create-user", false, "create a user when an unknown device token connects")
	flag.StringVar(&opts.setupToken, "setup-token", "", "require this token in POST /api/v1/setup (env: EASYTIER_SETUP_TOKEN)")
	flag.DurationVar(&opts.heartbeatMin, "heartbeat-interval", 3500*time.Millisecond, "heartbeat interval negotiated with devices")
	flag.DurationVar(&opts.heartbeatMax, "heartbeat-timeout", 15*time.Second, "heartbeat timeout before a session is dropped")
	flag.Parse()
	return opts
}

func run() error {
	opts := parseFlags()
	if opts.setupToken == "" {
		opts.setupToken = os.Getenv("EASYTIER_SETUP_TOKEN")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logging.Configure(os.Getenv("EASYTIER_LOG_LEVEL"), os.Stderr)
	store, report, err := db.Open(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	log.Printf("store ready: %s (schema %s)", store.Dialect(), report.Fingerprint)
	for _, change := range report.Changes {
		log.Printf("schema: %s", change)
	}
	if report.Legacy {
		log.Printf("schema: legacy tables detected; no data was modified")
	}
	if report.BackupPath != "" {
		log.Printf("schema: snapshot written to %s", report.BackupPath)
	}

	cfgLayer := settings.New(store)

	manager, err := session.NewManager(store, &session.LocalAuthenticator{
		Store:           store,
		AllowAutoCreate: opts.allowAutoCreate,
		AutoCreateAllowed: func(ctx context.Context, username string) error {
			if !cfgLayer.Bool(ctx, settings.KeyRegistrationOpen, true) {
				return bal.ErrNotFound
			}
			return cfgLayer.ValidateUsername(ctx, username)
		},
	}, session.Policy{Interval: opts.heartbeatMin, Timeout: opts.heartbeatMax})
	if err != nil {
		return err
	}

	if initialized, err := cfgLayer.Initialized(ctx); err == nil && !initialized && opts.setupToken == "" {
		log.Printf("WARNING: not initialized and no setup token; anyone who can reach this port can claim the admin account (set EASYTIER_SETUP_TOKEN)")
	}

	rpcListener, err := wire.ListenTCP(opts.listenRPC)
	if err != nil {
		return err
	}
	defer func() { _ = rpcListener.Close() }()
	log.Printf("config server listening on tcp://%s", rpcListener.Addr())

	var sessions sync.WaitGroup
	var wsListener wire.Listener
	if opts.listenWS != "" {
		wsListener, err = wire.ListenWS(opts.listenWS, func() bool {
			return cfgLayer.Bool(context.Background(), settings.KeyTrustProxy, false)
		})
		if err != nil {
			return err
		}
		defer func() { _ = wsListener.Close() }()
		go acceptLoop(ctx, wsListener, manager, &sessions)
		log.Printf("config server listening on ws://%s", wsListener.Addr())
	}
	go acceptLoop(ctx, rpcListener, manager, &sessions)

	var provider *oidc.Provider
	oidcConfig, oidcEnabled, err := oidc.FromEnv()
	if err != nil {
		return err
	}
	if oidcEnabled {
		provider, err = oidc.New(ctx, oidcConfig)
		if err != nil {
			return err
		}
		log.Printf("oidc enabled: issuer=%s client=%s", oidcConfig.Issuer, oidcConfig.ClientID)
	}

	api, err := httpapi.New(httpapi.Config{
		Store:            store,
		Sessions:         manager,
		SecureCookies:    opts.secureCookies,
		SetupToken:       opts.setupToken,
		WebDir:           opts.webDir,
		OIDC:             provider,
		Settings:         cfgLayer,
		ConfigServerPort: rpcPort(opts.listenRPC),
	})
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              opts.listenHTTP,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Bind before announcing anything: a port that is already taken has to stop
	// the process, not leave it running with a console nobody can reach.
	httpListener, err := net.Listen("tcp", opts.listenHTTP)
	if err != nil {
		return fmt.Errorf("http listener: %w", err)
	}
	defer httpListener.Close()
	go func() {
		if err := server.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server: %v", err)
			stop()
		}
	}()

	if cfgLayer.Bool(ctx, settings.KeyExtraTCPEnabled, false) {
		if port := cfgLayer.Int(ctx, settings.KeyExtraPort, 0); port > 0 {
			extra, err := wire.ListenTCP(fmt.Sprintf("0.0.0.0:%d", port))
			if err != nil {
				// The administrator turned this listener on; staying up without it would
				// leave devices pointed at a port nobody is serving.
				return fmt.Errorf("extra tcp listener: %w", err)
			}
			defer func() { _ = extra.Close() }()
			go acceptLoop(ctx, extra, manager, &sessions)
			log.Printf("extra tcp listener on %s", extra.Addr())
		}
	}
	if cfgLayer.Bool(ctx, settings.KeyExtraUDPEnabled, false) {
		// Enabled with no port used to fall through every branch: no bind, no
		// error, no log, while the console showed the switch as on (r150).
		if cfgLayer.Int(ctx, settings.KeyExtraPort, 0) <= 0 {
			return fmt.Errorf("extra udp listener enabled without a port; set listener.extra_port")
		}
		if port := cfgLayer.Int(ctx, settings.KeyExtraPort, 0); port > 0 {
			udpListener, err := wire.ListenUDP(fmt.Sprintf("0.0.0.0:%d", port))
			if err != nil {
				// Same rule as the TCP listener: a port the administrator asked for
				// must either serve or stop the process.
				return fmt.Errorf("extra udp listener: %w", err)
			}
			defer func() { _ = udpListener.Close() }()
			go acceptLoop(ctx, udpListener, manager, &sessions)
			log.Printf("extra udp listener on %s", udpListener.Addr())
		}
	}

	// Announce only after every configured listener is bound: a log line that says
	// "listening" while startup is about to fail is worse than no line at all.
	log.Printf("console listening on http://%s", httpListener.Addr())

	enforcer := &central.Reconciler{
		Store: store, Sessions: manager, Logf: log.Printf, Instances: manager,
		Interval: 5 * time.Second,
		// The push side honours the same quota as the report side.
		MaxInstancesPerUser: func(c context.Context, _ int64) int {
			return cfgLayer.MaxInstancesPerUser(c)
		},
	}
	// Without this the central-network feature would compile plans and never
	// deliver them: every push test calls the reconciler directly.
	go enforcer.Run(ctx)
	log.Printf("central network reconciler started (interval 5s)")
	// A delete is a revocation: let the API push immediately instead of waiting for
	// the next tick, so a 204 means the device has been told.
	api.SetReconcileNow(func(pushCtx context.Context) {
		if _, err := enforcer.ReconcileAll(pushCtx); err != nil {
			log.Printf("immediate reconcile after revocation: %v", err)
		}
	})

	// Record this process and warn if the database is shared with another
	// console: device tunnels live in memory, so a second replica would see half
	// the fleet as offline.
	instanceID, idErr := auth.RandomToken(12)
	if idErr != nil {
		return idErr
	}
	warnedSharedDatabase := false
	reportInstance := func() {
		if err := store.TouchInstance(ctx, instanceID, "easytier-console"); err != nil {
			log.Printf("instance heartbeat: %v", err)
			return
		}
		live, err := store.LiveInstances(ctx, 30*time.Second)
		if err == nil && live > 1 && !warnedSharedDatabase {
			log.Printf("WARNING: %d console instances share this database; device tunnels are per process, so device control and online state are NOT shared", live)
			warnedSharedDatabase = true
		}
	}
	reportInstance()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Stop accepting first, then let the sessions finish: an in-process
			// restart has to see the port free and the goroutines gone.
			log.Printf("shutting down")
			_ = rpcListener.Close()
			if wsListener != nil {
				_ = wsListener.Close()
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = server.Shutdown(shutdownCtx)
			cancel()
			drained := make(chan struct{})
			go func() {
				sessions.Wait()
				close(drained)
			}()
			select {
			case <-drained:
			case <-time.After(5 * time.Second):
				log.Printf("shutdown: %d session(s) did not finish in time", 1)
			}
			log.Printf("stopped")
			return nil
		case <-ticker.C:
			if dropped := manager.SweepStale(); dropped > 0 {
				log.Printf("dropped %d stale session(s)", dropped)
			}
			_, _ = store.PurgeExpiredSessions(ctx)
			_, _ = store.DeleteStaleInstances(ctx, 24*time.Hour)
			reportInstance()
			// Quota is enforced on the report path too: a device that starts
			// instances on its own is trimmed back to the allowance.
			if report, err := enforcer.EnforceQuotaAll(ctx, manager, cfgLayer.MaxInstancesPerUser); err != nil {
				log.Printf("quota enforcement: %v", err)
			} else if report.Trimmed > 0 {
				log.Printf("quota: trimmed %d instance(s) across %d machine(s)", report.Trimmed, report.Machines)
			}
		}
	}
}

func acceptLoop(ctx context.Context, listener wire.Listener, manager *session.Manager, sessions *sync.WaitGroup) {
	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("accept: %v", err)
			continue
		}
		sessions.Add(1)
		go func(tunnel wire.Tunnel) {
			defer sessions.Done()
			defer func() { _ = tunnel.Close() }()
			if err := manager.Serve(ctx, tunnel); err != nil && ctx.Err() == nil {
				log.Printf("session %s closed: %v", tunnel.RemoteAddr(), err)
			}
		}(conn)
	}
}
