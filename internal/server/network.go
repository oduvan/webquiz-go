package server

// networkInterfaces is get_network_interfaces: the machine's addresses that
// are not loopback (IPv4 only unless includeIPv6), for the "URL for other
// devices" list of the admin page. Task 11 ports it (internal/netinfo);
// until then the list is empty.
var networkInterfaces = func(includeIPv6 bool) []string {
	_ = includeIPv6
	return nil
}
