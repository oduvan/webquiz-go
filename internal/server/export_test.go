package server

// SetTransferEndpoint gives the server a public address as if its tunnel
// were connected: baseURL is the (fake) tunnel server, name its socket
// name. Tests of the file transfer between two servers use it.
func (s *Server) SetTransferEndpoint(baseURL, name string, connected bool) {
	endpoint := func() (string, string, bool) { return baseURL, name, connected }
	s.endpointForTests.Store(&endpoint)
}
