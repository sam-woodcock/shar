package server

// Option represents a SHAR server option
type Option interface {
	configure(server *Server)
}

// EphemeralStorage instructs SHAR to use memory rather than disk for storage.
// This is not recommended for production use.
func EphemeralStorage() ephemeralStorageOption {
	return ephemeralStorageOption{}
}

type ephemeralStorageOption struct{}

func (o ephemeralStorageOption) configure(server *Server) {
	server.ephemeralStorage = true
}

// PanicRecovery enables or disables SHAR's ability to recover from server panics.
// This is on by default, and disabling it is not recommended for production use.
func PanicRecovery(enabled bool) panicOption {
	return panicOption{value: enabled}
}

type panicOption struct{ value bool }

func (o panicOption) configure(server *Server) {
	server.panicRecovery = o.value
}
