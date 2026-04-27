package kiro

import (
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

func newHTTPClientWithProxyURL(cfg *config.Config, proxyURL string, timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}

	var sdkCfg config.SDKConfig
	if cfg != nil {
		sdkCfg = cfg.SDKConfig
	}
	if trimmed := strings.TrimSpace(proxyURL); trimmed != "" {
		sdkCfg.ProxyURL = trimmed
	}

	return util.SetProxy(&sdkCfg, client)
}
