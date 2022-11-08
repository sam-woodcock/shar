package server

// Option represents a SHAR server option
type Option interface {
	configure(server *Server)
}

// EphemeralStorage instructs SHAR to use memory rather than disk for storage.
// This is not recommended for production use.
func EphemeralStorage() EphemeralStorageOption {
	return EphemeralStorageOption{}
}

type EphemeralStorageOption struct{}

func (o EphemeralStorageOption) configure(server *Server) {
	server.ephemeralStorage = true
}

// PanicRecovery enables or disables SHAR's ability to recover from server panics.
// This is on by default, and disabling it is not recommended for production use.
func PanicRecovery(enabled bool) PanicOption {
	return PanicOption{value: enabled}
}

type PanicOption struct{ value bool }

func (o PanicOption) configure(server *Server) {
	server.panicRecovery = o.value
}

// PreventOrphanServiceTasks enables or disables SHAR's validation of service task names againt existing workflows.
func PreventOrphanServiceTasks() OrphanTaskOption {
	return OrphanTaskOption{value: true}
}

type OrphanTaskOption struct{ value bool }

func (o OrphanTaskOption) configure(server *Server) {
	server.allowOrphanServiceTasks = o.value
}
