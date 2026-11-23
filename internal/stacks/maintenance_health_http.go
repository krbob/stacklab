package stacks

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

type maintenanceHTTPCheck struct {
	path *url.URL
	port string
}

func newMaintenanceHTTPCheck(labels map[string]string) (*maintenanceHTTPCheck, error) {
	path, hasPath := labels["io.stacklab.health.http"]
	port, hasPort := labels["io.stacklab.health.port"]
	if !hasPath && !hasPort {
		return nil, nil
	}
	u, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(u.Path, "//") ||
		u.Host != "" || u.Scheme != "" || strings.ContainsAny(path, "?#") || strings.Contains(u.Path, "\\") ||
		strings.IndexFunc(u.Path, unicode.IsControl) != -1 {
		return nil, errors.New("invalid HTTP readiness path")
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return nil, errors.New("invalid HTTP readiness port")
	}
	return &maintenanceHTTPCheck{path: u, port: strconv.FormatUint(portNumber, 10)}, nil
}

var maintenanceHTTPClient = &http.Client{
	// Use only inspected addresses. Redirects and environment proxy settings
	// would allow a container to turn a readiness check into an off-host request.
	Transport:     &http.Transport{DisableKeepAlives: true, MaxResponseHeaderBytes: 8 << 10},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Timeout:       2 * time.Second,
}

func (check maintenanceHTTPCheck) probe(ctx context.Context, c dockerInspectContainer, client *http.Client) maintenanceHTTPResult {
	mode := c.HostConfig.NetworkMode
	if mode == "" || mode == "host" || mode == "none" || strings.HasPrefix(mode, "container:") || strings.HasPrefix(mode, "service:") {
		return maintenanceHTTPResult{note: "HTTP readiness unavailable for this network mode"}
	}
	var addresses []string
	for _, network := range c.NetworkSettings.Networks {
		for _, addr := range []string{network.IPAddress, network.GlobalIPv6Address} {
			ip, err := netip.ParseAddr(addr)
			if err == nil && ip.Zone() == "" && ip.Unmap().IsPrivate() {
				addresses = append(addresses, ip.Unmap().String())
			}
		}
	}
	if len(addresses) == 0 {
		return maintenanceHTTPResult{note: "HTTP readiness has no private container address"}
	}
	sort.Strings(addresses)
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	allRefused := true
	for _, address := range addresses {
		u := *check.path
		u.Scheme = "http"
		u.Host = net.JoinHostPort(address, check.port)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return maintenanceHTTPResult{note: "invalid HTTP readiness request"}
		}
		response, err := client.Do(req)
		if err != nil {
			allRefused = allRefused && errors.Is(err, syscall.ECONNREFUSED)
			continue
		}
		response.Body.Close()
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return maintenanceHTTPResult{note: "HTTP ready", ready: true}
		}
		return maintenanceHTTPResult{note: "HTTP readiness returned " + strconv.Itoa(response.StatusCode), regression: true}
	}
	return maintenanceHTTPResult{note: "HTTP readiness unreachable", regression: allRefused}
}
