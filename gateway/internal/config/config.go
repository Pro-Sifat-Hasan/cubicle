// Package config loads gateway settings from defaults, an optional config.json
// in the Cubicle home directory, and CUBICLE_* environment variables (highest priority).
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/fsutil"
)

const DefaultAddr = "127.0.0.1:7878"

type Config struct {
	Home                string   `json:"-"`
	Addr                string   `json:"addr"`
	TLSCert             string   `json:"tls_cert"`
	TLSKey              string   `json:"tls_key"`
	AllowInsecureRemote bool     `json:"allow_insecure_remote"`
	DailyTokenBudget    int64    `json:"daily_token_budget"`
	WorkerCommand       string   `json:"worker_command"`
	WorkerArgs          []string `json:"worker_args"`
	DisableWorker       bool     `json:"disable_worker"`
	RateLimitPerMinute  int      `json:"rate_limit_per_minute"`

	AdminToken  string `json:"-"`
	WorkerToken string `json:"-"`
}

// DefaultHome returns the per-user data directory, or the machine-wide one when
// running as a Windows service (serviceMode).
func DefaultHome(serviceMode bool) string {
	if h := os.Getenv("CUBICLE_HOME"); h != "" {
		return h
	}
	if runtime.GOOS == "windows" {
		if serviceMode {
			if pd := os.Getenv("ProgramData"); pd != "" {
				return filepath.Join(pd, "Cubicle")
			}
		}
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			return filepath.Join(la, "Cubicle")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cubicle")
	}
	return ".cubicle"
}

func Load(serviceMode bool) (*Config, error) {
	c := &Config{
		Home:               DefaultHome(serviceMode),
		Addr:               DefaultAddr,
		RateLimitPerMinute: 600,
	}
	if err := fsutil.EnsurePrivateDir(c.Home); err != nil {
		return nil, fmt.Errorf("create home %s: %w", c.Home, err)
	}

	if b, err := os.ReadFile(filepath.Join(c.Home, "config.json")); err == nil {
		if err := json.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("parse config.json: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	envStr("CUBICLE_ADDR", &c.Addr)
	envStr("CUBICLE_TLS_CERT", &c.TLSCert)
	envStr("CUBICLE_TLS_KEY", &c.TLSKey)
	envStr("CUBICLE_WORKER_COMMAND", &c.WorkerCommand)
	envBool("CUBICLE_ALLOW_INSECURE_REMOTE", &c.AllowInsecureRemote)
	envBool("CUBICLE_DISABLE_WORKER", &c.DisableWorker)
	if v := os.Getenv("CUBICLE_DAILY_TOKEN_BUDGET"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("CUBICLE_DAILY_TOKEN_BUDGET: %w", err)
		}
		c.DailyTokenBudget = n
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	var err error
	if c.AdminToken, err = loadOrCreateToken(filepath.Join(c.Home, "admin.token"), os.Getenv("CUBICLE_ADMIN_TOKEN")); err != nil {
		return nil, err
	}
	if c.WorkerToken, err = loadOrCreateToken(filepath.Join(c.Home, "worker.token"), os.Getenv("CUBICLE_WORKER_TOKEN")); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) TLSEnabled() bool { return c.TLSCert != "" && c.TLSKey != "" }

// validate refuses to expose the API beyond loopback without TLS unless the
// operator explicitly opts in (e.g. behind a TLS-terminating reverse proxy).
func (c *Config) validate() error {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return fmt.Errorf("invalid addr %q: %w", c.Addr, err)
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("tls_cert and tls_key must be set together")
	}
	if !isLoopback(host) && !c.TLSEnabled() && !c.AllowInsecureRemote {
		return fmt.Errorf("refusing to listen on non-loopback address %q without TLS; set tls_cert/tls_key or CUBICLE_ALLOW_INSECURE_REMOTE=1", c.Addr)
	}
	return nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loadOrCreateToken(path, override string) (string, error) {
	if override != "" {
		if len(override) < 32 {
			return "", fmt.Errorf("token override for %s must be at least 32 characters", filepath.Base(path))
		}
		return override, nil
	}
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); len(t) >= 32 {
			return t, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	if err := fsutil.WritePrivateFile(path, []byte(tok)); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return tok, nil
}

func envStr(key string, dst *string) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		*dst = v
	}
}

func envBool(key string, dst *bool) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		*dst = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
}
