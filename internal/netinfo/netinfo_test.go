package netinfo

import (
	"errors"
	"slices"
	"testing"

	"github.com/oduvan/webquiz-go/internal/config"
)

// fakeSources stands in for the mocks of the Python tests (gethostname,
// getaddrinfo, platform.system and "hostname -I").
func fakeSources(goos string, lookup []string, hostnameI string) Sources {
	return Sources{
		Hostname:   func() (string, error) { return "testhost", nil },
		LookupIPv4: func(string) ([]string, error) { return lookup, nil },
		OS:         goos,
		HostnameI:  func() (string, error) { return hostnameI, nil },
	}
}

func TestIsLoopbackAddress(t *testing.T) {
	cases := []struct {
		py       string
		ips      []string
		loopback bool
	}{
		{py: "tests/test_network_interfaces.py::TestIsLoopbackAddress::test_ipv4_localhost", ips: []string{"127.0.0.1"}, loopback: true},
		{py: "tests/test_network_interfaces.py::TestIsLoopbackAddress::test_ipv4_loopback_range", ips: []string{"127.0.0.2", "127.0.1.1", "127.255.255.255"}, loopback: true},
		{py: "tests/test_network_interfaces.py::TestIsLoopbackAddress::test_ipv6_loopback", ips: []string{"::1"}, loopback: true},
		{py: "tests/test_network_interfaces.py::TestIsLoopbackAddress::test_private_ip_not_loopback", ips: []string{"192.168.1.1", "10.0.0.1", "172.16.0.1"}, loopback: false},
		{py: "tests/test_network_interfaces.py::TestIsLoopbackAddress::test_public_ip_not_loopback", ips: []string{"8.8.8.8", "1.1.1.1"}, loopback: false},
		{py: "tests/test_network_interfaces.py::TestIsLoopbackAddress::test_invalid_ip", ips: []string{"not-an-ip", ""}, loopback: true},
	}
	for _, c := range cases {
		for _, ip := range c.ips {
			if got := IsLoopbackAddress(ip); got != c.loopback {
				t.Errorf("%s: IsLoopbackAddress(%q) = %v", c.py, ip, got)
			}
		}
	}
}

// py: tests/test_network_interfaces.py::TestGetNetworkInterfaces::test_filters_localhost
func TestFiltersLocalhost(t *testing.T) {
	got := fakeSources("windows", []string{"127.0.0.1", "192.168.1.100"}, "").Interfaces(false)
	if slices.Contains(got, "127.0.0.1") || !slices.Contains(got, "192.168.1.100") {
		t.Errorf("got %v", got)
	}
}

// py: tests/test_network_interfaces.py::TestGetNetworkInterfaces::test_filters_loopback_range
func TestFiltersLoopbackRange(t *testing.T) {
	got := fakeSources("windows", []string{"127.0.0.2", "127.0.1.1", "10.0.0.5"}, "").Interfaces(false)
	if !slices.Equal(got, []string{"10.0.0.5"}) {
		t.Errorf("got %v", got)
	}
}

// py: tests/test_network_interfaces.py::TestGetNetworkInterfaces::test_filters_loopback_from_hostname_command
func TestFiltersLoopbackFromHostnameCommand(t *testing.T) {
	got := fakeSources("linux", nil, "127.0.0.1 192.168.1.50 10.0.0.10\n").Interfaces(false)
	if slices.Contains(got, "127.0.0.1") || !slices.Contains(got, "192.168.1.50") || !slices.Contains(got, "10.0.0.10") {
		t.Errorf("got %v", got)
	}
}

// py: tests/test_network_interfaces.py::TestGetNetworkInterfaces::test_empty_when_only_loopback
func TestEmptyWhenOnlyLoopback(t *testing.T) {
	got := fakeSources("windows", []string{"127.0.0.1"}, "").Interfaces(false)
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// py: tests/test_network_interfaces.py::TestGetNetworkInterfaces::test_filters_ipv6_by_default
func TestFiltersIPv6ByDefault(t *testing.T) {
	got := fakeSources("linux", []string{"192.168.1.100"}, "192.168.1.100 fe80::1 2001:db8::1").Interfaces(false)
	if !slices.Equal(got, []string{"192.168.1.100"}) {
		t.Errorf("got %v", got)
	}
}

// py: tests/test_network_interfaces.py::TestGetNetworkInterfaces::test_includes_ipv6_when_enabled
func TestIncludesIPv6WhenEnabled(t *testing.T) {
	got := fakeSources("linux", []string{"192.168.1.100"}, "192.168.1.100 fe80::1 2001:db8::1").Interfaces(true)
	for _, want := range []string{"192.168.1.100", "fe80::1", "2001:db8::1"} {
		if !slices.Contains(got, want) {
			t.Errorf("%s missing from %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("an address listed twice is shown once: %v", got)
	}
}

// py: tests/test_network_interfaces.py::TestGetNetworkInterfaces::test_filters_ipv6_explicitly_disabled
func TestFiltersIPv6ExplicitlyDisabled(t *testing.T) {
	got := fakeSources("linux", nil, "10.0.0.5 fe80::abc:def").Interfaces(false)
	if !slices.Equal(got, []string{"10.0.0.5"}) {
		t.Errorf("got %v", got)
	}
}

// Not a Python test: a failing source is skipped, the other still counts.
func TestFailingSourcesAreSkipped(t *testing.T) {
	src := fakeSources("linux", nil, "10.0.0.5")
	src.LookupIPv4 = func(string) ([]string, error) { return nil, errors.New("no such host") }
	if got := src.Interfaces(false); !slices.Equal(got, []string{"10.0.0.5"}) {
		t.Errorf("got %v", got)
	}
	src.HostnameI = func() (string, error) { return "", errors.New("no hostname command") }
	if got := src.Interfaces(false); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// py: tests/test_network_interfaces.py::TestUrlFormat::test_default_url_format
func TestDefaultURLFormat(t *testing.T) {
	format := config.Default().Server.URLFormat
	if format != "http://{IP}:{PORT}/" {
		t.Errorf("default format %q", format)
	}
	if got := FormatURL(format, "192.168.1.100", 8080); got != "http://192.168.1.100:8080/" {
		t.Errorf("got %q", got)
	}
}

// py: tests/test_network_interfaces.py::TestUrlFormat::test_custom_url_format_without_port
func TestCustomURLFormatWithoutPort(t *testing.T) {
	if got := FormatURL("http://{IP}/webquiz/", "192.168.1.100", 8080); got != "http://192.168.1.100/webquiz/" {
		t.Errorf("got %q", got)
	}
}

// py: tests/test_network_interfaces.py::TestUrlFormat::test_custom_url_format_with_https
func TestCustomURLFormatWithHTTPS(t *testing.T) {
	if got := FormatURL("https://{IP}:{PORT}/quiz/", "10.0.0.5", 443); got != "https://10.0.0.5:443/quiz/" {
		t.Errorf("got %q", got)
	}
}

// py: tests/test_network_interfaces.py::TestUrlFormat::test_url_format_preserves_placeholders_case
func TestURLFormatPreservesPlaceholdersCase(t *testing.T) {
	if got := FormatURL("http://{ip}:{port}/", "192.168.1.100", 8080); got != "http://{ip}:{port}/" {
		t.Errorf("got %q", got)
	}
}

// py: tests/test_network_interfaces.py::TestPort80Normalization::test_normalize_url_removes_port_80
func TestNormalizeURLRemovesPort80(t *testing.T) {
	if got := NormalizeURL("http://192.168.1.100:80/"); got != "http://192.168.1.100/" {
		t.Errorf("got %q", got)
	}
	if got := NormalizeURL("http://10.0.0.5:80/quiz/"); got != "http://10.0.0.5/quiz/" {
		t.Errorf("got %q", got)
	}
	// The port 80 given as the format's port also goes.
	if got := FormatURL("http://{IP}:{PORT}/", "10.0.0.5", 80); got != "http://10.0.0.5/" {
		t.Errorf("got %q", got)
	}
}

// py: tests/test_network_interfaces.py::TestPort80Normalization::test_normalize_url_preserves_other_ports
func TestNormalizeURLPreservesOtherPorts(t *testing.T) {
	for _, url := range []string{"http://192.168.1.100:8080/", "http://10.0.0.5:3000/", "http://10.0.0.5:81/"} {
		if got := NormalizeURL(url); got != url {
			t.Errorf("NormalizeURL(%q) = %q", url, got)
		}
	}
}

// py: tests/test_network_interfaces.py::TestPort80Normalization::test_normalize_url_does_not_affect_8080
func TestNormalizeURLDoesNotAffect8080(t *testing.T) {
	for _, url := range []string{"http://192.168.1.100:8080/", "http://192.168.1.100:800/"} {
		if got := NormalizeURL(url); got != url {
			t.Errorf("NormalizeURL(%q) = %q", url, got)
		}
	}
}

// The real sources run without error on this machine.
func TestSystemInterfacesDoNotFail(t *testing.T) {
	for _, ip := range Interfaces(true) {
		if IsLoopbackAddress(ip) {
			t.Errorf("loopback address %s in the list", ip)
		}
	}
}
