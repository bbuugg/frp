// Copyright 2025 The frp-panel Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package panel provides a client that connects frps to the frp-panel management
// panel. It mirrors the keep-alive and exponential back-off reconnect logic used
// by frpc to maintain a control connection with frps.
package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"

	"github.com/fatedier/frp/pkg/util/log"
	"github.com/fatedier/frp/pkg/util/wait"
)

// ─── Message types ─────────────────────────────────────────────────────────────

type MsgType string

const (
	MsgTypeLogin         MsgType = "login"
	MsgTypeLoginResp     MsgType = "login_resp"
	MsgTypePing          MsgType = "ping"
	MsgTypePong          MsgType = "pong"
	MsgTypeFetchTraffic  MsgType = "fetch_traffic"
	MsgTypeTrafficReport MsgType = "traffic_report"
	MsgTypeReloadConfig  MsgType = "reload_config"
)

// BaseMsg is the envelope for all messages over the WebSocket connection.
type BaseMsg struct {
	Type MsgType `json:"type"`
}

// LoginMsg is sent by frps immediately after the WebSocket connection is
// established. The panel validates the secret and replies with LoginRespMsg.
type LoginMsg struct {
	Type    MsgType `json:"type"`
	Secret  string  `json:"secret"`
	Version string  `json:"version"`
}

// LoginRespMsg is sent by the panel in response to LoginMsg.
type LoginRespMsg struct {
	Type    MsgType `json:"type"`
	Success bool    `json:"success"`
	Error   string  `json:"error,omitempty"`
}

// TrafficReportMsg carries live statistics back to the panel.
type TrafficReportMsg struct {
	Type            MsgType `json:"type"`
	TotalTrafficIn  int64   `json:"total_traffic_in"`
	TotalTrafficOut int64   `json:"total_traffic_out"`
	CurConns        int64   `json:"cur_conns"`
	ClientCount     int64   `json:"client_count"`
	ProxyCount      int64   `json:"proxy_count"`
}

// ─── Stats provider ────────────────────────────────────────────────────────────

// StatsGetter is implemented by the frps Service to expose live statistics.
type StatsGetter interface {
	GetTotalTrafficIn() int64
	GetTotalTrafficOut() int64
	GetCurConns() int64
	GetClientCount() int64
	GetProxyCount() int64
}

// ─── Connector ─────────────────────────────────────────────────────────────────

// Connector maintains a persistent WebSocket connection from frps to the panel.
// When panel-url and panel-secret are both non-empty, Start() will keep the
// connection alive with the same back-off strategy used by frpc.
type Connector struct {
	panelURL string
	secret   string
	stats    StatsGetter

	ctx    context.Context
	cancel context.CancelFunc

	lastPong atomic.Value // stores time.Time
}

// NewConnector returns a new Connector. Call Start() to begin the connection loop.
func NewConnector(ctx context.Context, panelURL, secret string, stats StatsGetter) *Connector {
	ctx, cancel := context.WithCancel(ctx)
	c := &Connector{
		panelURL: panelURL,
		secret:   secret,
		stats:    stats,
		ctx:      ctx,
		cancel:   cancel,
	}
	c.lastPong.Store(time.Now())
	return c
}

// Start launches the background goroutine. It returns immediately.
func (c *Connector) Start() {
	go c.run()
}

// Stop terminates the connection loop.
func (c *Connector) Stop() {
	c.cancel()
}

// run is the outer connect-loop. It calls connectOnce and retries on failure
// using the same FastBackoff parameters that frpc uses for reconnecting to frps.
func (c *Connector) run() {
	wait.BackoffUntil(func() (bool, error) {
		if err := c.connectOnce(); err != nil {
			log.Warnf("[panel] connection to panel lost: %v – will reconnect", err)
			return false, err
		}
		return false, fmt.Errorf("session ended normally")
	}, wait.NewFastBackoffManager(wait.FastBackoffOptions{
		Duration:        time.Second,
		Factor:          2,
		Jitter:          0.1,
		MaxDuration:     20 * time.Second,
		FastRetryCount:  3,
		FastRetryDelay:  200 * time.Millisecond,
		FastRetryWindow: time.Minute,
		FastRetryJitter: 0.5,
	}), true, c.ctx.Done())

	log.Infof("[panel] connector stopped")
}

// connectOnce dials the panel, performs the login handshake, then enters a
// read-loop that processes inbound commands. Heart-beat pings are sent
// concurrently. The function returns when the connection is closed or the
// context is cancelled.
func (c *Connector) connectOnce() error {
	log.Infof("[panel] connecting to panel at %s", c.panelURL)

	origin := "http://frps"
	cfg, err := websocket.NewConfig(c.panelURL, origin)
	if err != nil {
		return fmt.Errorf("build ws config: %w", err)
	}
	cfg.Header = http.Header{}
	cfg.Header.Set("User-Agent", "frps-panel-connector/1.0")

	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	log.Infof("[panel] WebSocket connected, sending login")

	// ── Login ────────────────────────────────────────────────────────────────
	loginMsg := LoginMsg{
		Type:    MsgTypeLogin,
		Secret:  c.secret,
		Version: "frps",
	}
	if err := websocket.JSON.Send(conn, loginMsg); err != nil {
		return fmt.Errorf("send login: %w", err)
	}

	var loginResp LoginRespMsg
	if err := websocket.JSON.Receive(conn, &loginResp); err != nil {
		return fmt.Errorf("receive login_resp: %w", err)
	}
	if !loginResp.Success {
		return fmt.Errorf("panel rejected login: %s", loginResp.Error)
	}
	log.Infof("[panel] login successful")
	c.lastPong.Store(time.Now())

	// ── Heartbeat sender ─────────────────────────────────────────────────────
	heartbeatInterval := 30 * time.Second
	heartbeatTimeout := 90 * time.Second
	doneCh := make(chan struct{})
	defer close(doneCh)

	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ping := BaseMsg{Type: MsgTypePing}
				if sendErr := websocket.JSON.Send(conn, ping); sendErr != nil {
					log.Warnf("[panel] ping send error: %v", sendErr)
					conn.Close()
					return
				}
				// heartbeat timeout check
				if time.Since(c.lastPong.Load().(time.Time)) > heartbeatTimeout {
					log.Warnf("[panel] heartbeat timeout, closing connection")
					conn.Close()
					return
				}
			case <-doneCh:
				return
			case <-c.ctx.Done():
				return
			}
		}
	}()

	// ── Inbound message loop ──────────────────────────────────────────────────
	for {
		// Use a raw byte slice so we can inspect the type field first.
		var raw json.RawMessage
		if err := websocket.JSON.Receive(conn, &raw); err != nil {
			return fmt.Errorf("recv: %w", err)
		}

		var base BaseMsg
		if err := json.Unmarshal(raw, &base); err != nil {
			log.Warnf("[panel] unknown message: %s", string(raw))
			continue
		}

		switch base.Type {
		case MsgTypePong:
			c.lastPong.Store(time.Now())
			log.Debugf("[panel] pong received")

		case MsgTypeFetchTraffic:
			log.Debugf("[panel] fetch_traffic requested")
			report := c.buildTrafficReport()
			if sendErr := websocket.JSON.Send(conn, report); sendErr != nil {
				log.Warnf("[panel] send traffic_report error: %v", sendErr)
			}

		case MsgTypeReloadConfig:
			log.Infof("[panel] reload_config received (not yet implemented)")

		default:
			log.Debugf("[panel] unhandled message type: %s", base.Type)
		}

		// Check context cancellation between messages.
		select {
		case <-c.ctx.Done():
			return nil
		default:
		}
	}
}

func (c *Connector) buildTrafficReport() TrafficReportMsg {
	if c.stats == nil {
		return TrafficReportMsg{Type: MsgTypeTrafficReport}
	}
	return TrafficReportMsg{
		Type:            MsgTypeTrafficReport,
		TotalTrafficIn:  c.stats.GetTotalTrafficIn(),
		TotalTrafficOut: c.stats.GetTotalTrafficOut(),
		CurConns:        c.stats.GetCurConns(),
		ClientCount:     c.stats.GetClientCount(),
		ProxyCount:      c.stats.GetProxyCount(),
	}
}
