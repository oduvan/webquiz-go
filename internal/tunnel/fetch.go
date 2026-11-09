package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"

	"gopkg.in/yaml.v3"
)

// errNoServer is the error when the config names no tunnel server.
var errNoServer = errors.New("Tunnel server not configured")

// fetchTunnelConfig returns the tunnel server settings: the ones written in
// the config file if there are any, otherwise the ones in the server's
// tunnel_config.yaml. When the download fails, it puts the reason in the
// status error and returns it.
func (m *Manager) fetchTunnelConfig(ctx context.Context) (*ServerConfig, error) {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()

	if cfg.Config != nil {
		m.log().Info("Using local tunnel configuration from config file")
		local := *cfg.Config
		return &local, nil
	}
	if cfg.Server == "" {
		return nil, errNoServer
	}
	address := tunnelConfigURL(cfg.Server)
	serverConfig, err := m.download(ctx, address)
	if err != nil {
		m.log().Error(fmt.Sprintf("Failed to fetch tunnel config from %s: %v", address, err))
		message := "Failed to fetch config from server: " + err.Error()
		m.mu.Lock()
		m.status.Error = message
		m.mu.Unlock()
		return nil, errors.New(message)
	}
	m.log().Info("Fetched tunnel config from " + address)
	return serverConfig, nil
}

// tunnelConfigURL is the address of tunnel_config.yaml on a tunnel server:
// plain HTTP for an IP address, HTTPS for a host name.
func tunnelConfigURL(server string) string {
	address := url.URL{Scheme: "https", Host: server, Path: "/tunnel_config.yaml"}
	if ip, err := netip.ParseAddr(server); err == nil {
		address.Scheme = "http"
		if ip.Is6() {
			// A URL needs brackets around an IPv6 address. Python left them
			// out, so its request failed.
			address.Host = "[" + server + "]"
		}
	}
	return address.String()
}

// download gets tunnel_config.yaml and checks that it has the three fields
// WebQuiz needs.
func (m *Manager) download(ctx context.Context, address string) (*ServerConfig, error) {
	client := &http.Client{
		Transport: m.transport,
		Timeout:   m.fetchTimeout,
		// Python's httpx client did not follow redirects: a redirect is an error.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	// Python opened a new HTTP client for each download and closed it after.
	request.Close = true
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, statusError(response, address)
	}
	return parseServerConfig(body)
}

// statusError describes a reply outside 2xx in the words of httpx's
// raise_for_status, which Python showed in the admin panel.
func statusError(response *http.Response, address string) error {
	kind := "Invalid status code"
	switch response.StatusCode / 100 {
	case 1:
		kind = "Informational response"
	case 3:
		kind = "Redirect response"
	case 4:
		kind = "Client error"
	case 5:
		kind = "Server error"
	}
	return fmt.Errorf("%s '%s' for url '%s'\nFor more information check: https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/%d",
		kind, response.Status, address, response.StatusCode)
}

// parseServerConfig reads tunnel_config.yaml. Like Python, it needs the keys
// username, socket_directory and base_url, and ignores other keys.
func parseServerConfig(body []byte) (*ServerConfig, error) {
	var fields map[string]any
	if err := yaml.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	for _, name := range []string{"username", "socket_directory", "base_url"} {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("Missing required field in tunnel config: %s", name)
		}
	}
	var serverConfig ServerConfig
	if err := yaml.Unmarshal(body, &serverConfig); err != nil {
		return nil, err
	}
	return &serverConfig, nil
}
