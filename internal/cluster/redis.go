// Copyright 2026 the Dubash contributors. Apache-2.0.

// Package cluster coordinates exclusive room owners and proxies room traffic.
package cluster

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Redis implements only the small RESP2 command surface required for leases.
// Each command owns its connection, avoiding shared-socket races and blocking
// pools. All I/O has a hard deadline; rediss enables certificate-verified TLS.
type Redis struct{ URL string }

func (r Redis) Do(ctx context.Context, args ...string) (string, error) {
	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "redis" && u.Scheme != "rediss") || u.Hostname() == "" {
		return "", errors.New("REDIS_URL must use redis:// or rediss://")
	}
	addr := net.JoinHostPort(u.Hostname(), u.Port())
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "6379")
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	var conn net.Conn
	if u.Scheme == "rediss" {
		td := tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}}
		conn, err = td.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	if t, ok := ctx.Deadline(); ok && t.Before(deadline) {
		deadline = t
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return "", err
	}
	reader := bufio.NewReader(conn)
	command := func(a ...string) (string, error) {
		var b strings.Builder
		fmt.Fprintf(&b, "*%d\r\n", len(a))
		for _, s := range a {
			fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(s), s)
		}
		if _, err := io.WriteString(conn, b.String()); err != nil {
			return "", err
		}
		return readRESP(reader)
	}
	if u.User != nil {
		password, hasPassword := u.User.Password()
		if !hasPassword {
			password = u.User.Username()
		}
		auth := []string{"AUTH", password}
		if hasPassword && u.User.Username() != "" {
			auth = []string{"AUTH", u.User.Username(), password}
		}
		if _, err = command(auth...); err != nil {
			return "", err
		}
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != "" && db != "0" {
		if _, err = strconv.ParseUint(db, 10, 32); err != nil {
			return "", errors.New("invalid Redis database")
		}
		if _, err = command("SELECT", db); err != nil {
			return "", err
		}
	}
	return command(args...)
}

func readRESP(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) < 3 || !strings.HasSuffix(line, "\r\n") {
		return "", errors.New("malformed Redis reply")
	}
	value := line[1 : len(line)-2]
	switch line[0] {
	case '+', ':':
		return value, nil
	case '-':
		return "", fmt.Errorf("Redis: %s", value)
	case '$':
		n, err := strconv.Atoi(value)
		if err != nil || n < -1 || n > 1<<20 {
			return "", errors.New("invalid Redis bulk length")
		}
		if n == -1 {
			return "", nil
		}
		data := make([]byte, n+2)
		if _, err = io.ReadFull(r, data); err != nil {
			return "", err
		}
		if string(data[n:]) != "\r\n" {
			return "", errors.New("invalid Redis bulk terminator")
		}
		return string(data[:n]), nil
	default:
		return "", errors.New("unsupported Redis reply")
	}
}
