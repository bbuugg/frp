package main

import (
	"testing"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func TestValidatePanelConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *v1.ServerConfig
		wantErr bool
	}{
		{
			name:    "missing both",
			cfg:     &v1.ServerConfig{},
			wantErr: true,
		},
		{
			name: "missing token",
			cfg: &v1.ServerConfig{Panel: v1.PanelConnectorConfig{
				URL: "ws://127.0.0.1:7200/ws/node",
			}},
			wantErr: true,
		},
		{
			name: "missing url",
			cfg: &v1.ServerConfig{Panel: v1.PanelConnectorConfig{
				Token: "node-token",
			}},
			wantErr: true,
		},
		{
			name: "ok",
			cfg: &v1.ServerConfig{Panel: v1.PanelConnectorConfig{
				URL:   "ws://127.0.0.1:7200/ws/node",
				Token: "node-token",
			}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePanelConfig(tt.cfg)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
