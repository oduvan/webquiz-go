// Package netinfo finds the addresses of this machine that other devices can
// use to reach the server, and builds the URLs shown to the teacher. It is
// get_network_interfaces, is_loopback_address and normalize_url of
// webquiz/server.py.
package netinfo

import (
	"context"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// IsLoopbackAddress is is_loopback_address: 127.0.0.0/8, ::1 and their
// IPv4-mapped forms. Text that is not an address counts as loopback (Python
// did this to be safe: such an address is left out of lists).
func IsLoopbackAddress(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return true
	}
	return addr.WithZone("").Unmap().IsLoopback()
}

// NormalizeURL is normalize_url: it removes the default port 80 (":80/").
// Only ":80/" is replaced, so ":8080/" and ":800/" stay as they are.
func NormalizeURL(url string) string {
	return strings.ReplaceAll(url, ":80/", "/")
}

// FormatURL fills {IP} and {PORT} of server.url_format and then removes the
// port 80. Lowercase placeholders are not replaced.
func FormatURL(format, ip string, port int) string {
	url := strings.ReplaceAll(format, "{IP}", ip)
	url = strings.ReplaceAll(url, "{PORT}", strconv.Itoa(port))
	return NormalizeURL(url)
}

// Sources are the machine facts that Interfaces reads. Tests replace them.
type Sources struct {
	// Hostname is the name of this machine.
	Hostname func() (string, error)
	// LookupIPv4 gives the IPv4 addresses of a host name (getaddrinfo with AF_INET).
	LookupIPv4 func(host string) ([]string, error)
	// OS is runtime.GOOS: on "windows" the "hostname -I" command is not used.
	OS string
	// HostnameI runs "hostname -I" and gives its output.
	HostnameI func() (string, error)
}

// System are the real sources.
var System = Sources{
	Hostname:   hostname,
	LookupIPv4: lookupIPv4,
	OS:         runtime.GOOS,
	HostnameI:  hostnameI,
}

// Interfaces is get_network_interfaces: the addresses of this machine that
// are not loopback. IPv6 addresses are left out unless includeIPv6. Each
// address appears once, in the order found. The addresses come from the
// host name and, except on Windows, from "hostname -I". A source that fails
// is skipped (Python let a failed host name lookup raise; the callers only
// show the list, so a shorter list is better than an error page).
func Interfaces(includeIPv6 bool) []string {
	return System.Interfaces(includeIPv6)
}

// Interfaces is the same as the package function, with these sources.
func (src Sources) Interfaces(includeIPv6 bool) []string {
	var found []string
	seen := map[string]bool{}
	add := func(ip string) {
		if seen[ip] || IsLoopbackAddress(ip) {
			return
		}
		if !includeIPv6 && strings.Contains(ip, ":") {
			return
		}
		seen[ip] = true
		found = append(found, ip)
	}

	if name, err := src.Hostname(); err == nil {
		if ips, err := src.LookupIPv4(name); err == nil {
			for _, ip := range ips {
				add(ip)
			}
		}
	}
	if src.OS != "windows" {
		if out, err := src.HostnameI(); err == nil {
			for _, ip := range strings.Fields(out) {
				add(ip)
			}
		}
	}
	return found
}

func hostname() (string, error) {
	return os.Hostname()
}

func lookupIPv4(host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	list := make([]string, 0, len(ips))
	for _, ip := range ips {
		list = append(list, ip.String())
	}
	return list, nil
}

func hostnameI() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "hostname", "-I").Output()
	return string(out), err
}
