// Command linuxquest runs the LinuxQuest SSH server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"

	missionsdata "linuxquest/missions"

	"linuxquest/internal/missions"
	"linuxquest/internal/sshserver"
	"linuxquest/internal/store"
	"linuxquest/internal/webhelp"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
		log.Warn("ignoring invalid value", "var", key)
	}
	return def
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local SSH port and exit 0 if it answers")
	flag.Parse()

	host := env("LINUXQUEST_HOST", "0.0.0.0")
	port := env("LINUXQUEST_PORT", "2222")
	hostKeyPath := env("LINUXQUEST_HOST_KEY", "data/ssh_host_ed25519")

	if *healthcheck {
		os.Exit(probe(port))
	}

	catalog, err := missions.Load(missionsdata.FS)
	if err != nil {
		log.Fatal("load missions", "err", err)
	}

	dbPath := env("LINUXQUEST_DB", "data/linuxquest.db")
	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatal("open account database", "err", err)
	}
	defer db.Close()

	srv, err := sshserver.New(sshserver.Config{
		Host:        host,
		Port:        port,
		HostKeyPath: hostKeyPath,
		MaxSessions: envInt("LINUXQUEST_MAX_SESSIONS", 100),
		IdleTimeout: time.Duration(envInt("LINUXQUEST_IDLE_TIMEOUT_MIN", 15)) * time.Minute,
		MaxDuration: time.Duration(envInt("LINUXQUEST_MAX_SESSION_MIN", 120)) * time.Minute,
		Store:       db,
		Admins:      adminSet(env("LINUXQUEST_ADMINS", "")),
	}, catalog)
	if err != nil {
		log.Fatal("create server", "err", err)
	}

	webPort := env("LINUXQUEST_WEB_PORT", "8080")
	var web *http.Server
	if webPort != "" && webPort != "0" {
		web = &http.Server{
			Addr:              net.JoinHostPort("0.0.0.0", webPort),
			Handler:           webhelp.New(hostKeyPath, port).Handler(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			if err := web.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("web server stopped", "err", err)
			}
		}()
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	log.Info("LinuxQuest listening", "addr", srv.Addr(), "missions", len(catalog), "web", webPort)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("server stopped", "err", err)
			done <- syscall.SIGTERM
		}
	}()

	<-done
	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if web != nil {
		_ = web.Shutdown(ctx)
	}
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("shutdown", "err", err)
	}
}

// adminSet parses a comma-separated list of admin usernames.
func adminSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, u := range strings.Split(s, ",") {
		if u = strings.TrimSpace(u); u != "" {
			out[u] = true
		}
	}
	return out
}

// probe connects to the local port and checks for an SSH banner.
func probe(port string) int {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 3*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4)
	if _, err := conn.Read(buf); err != nil || string(buf) != "SSH-" {
		fmt.Fprintln(os.Stderr, "healthcheck: no SSH banner")
		return 1
	}
	return 0
}
