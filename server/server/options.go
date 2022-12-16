package server

import (
	"gitlab.com/shar-workflow/shar/common/authn"
	"gitlab.com/shar-workflow/shar/common/authz"
)

// Option represents a SHAR server option
type Option interface {
	configure(server *Server)
}

// EphemeralStorage instructs SHAR to use memory rather than disk for storage.
// This is not recommended for production use.
func EphemeralStorage() ephemeralStorageOption { //nolint
	return ephemeralStorageOption{}
}

type ephemeralStorageOption struct {
}

func (o ephemeralStorageOption) configure(server *Server) {
	server.ephemeralStorage = true
}

// PanicRecovery enables or disables SHAR's ability to recover from server panics.
// This is on by default, and disabling it is not recommended for production use.
func PanicRecovery(enabled bool) panicOption { //nolint
	return panicOption{value: enabled}
}

type panicOption struct{ value bool }

func (o panicOption) configure(server *Server) {
	server.panicRecovery = o.value
}

// PreventOrphanServiceTasks enables or disables SHAR's validation of service task names againt existing workflows.
func PreventOrphanServiceTasks() orphanTaskOption { //nolint
	return orphanTaskOption{value: true}
}

type orphanTaskOption struct{ value bool }

func (o orphanTaskOption) configure(server *Server) {
	server.allowOrphanServiceTasks = o.value
}

// Concurrency specifies the number of threads for each of SHAR's queue listeneres.
func Concurrency(n int) concurrencyOption { //nolint
	return concurrencyOption{value: n}
}

type concurrencyOption struct{ value int }

func (o concurrencyOption) configure(server *Server) {
	server.concurrency = o.value
}

// WithApiAuthorizer specifies a handler function for API authorization.
func WithApiAuthorizer(authFn authz.APIFunc) apiAuthorizerOption { //nolint
	return apiAuthorizerOption{value: authFn}
}

type apiAuthorizerOption struct{ value authz.APIFunc }

func (o apiAuthorizerOption) configure(server *Server) {
	server.apiAuthorizer = o.value
}


// WithAuthentication specifies a handler function for API authorization.
func WithAuthentication(authFn authn.Check) authenticationOption { //nolint
	return authenticationOption{value: authFn}
}

type authenticationOption struct{ value authn.Check }

func (o authenticationOption) configure(server *Server) {
	server.apiAuthenticator = o.value
}
