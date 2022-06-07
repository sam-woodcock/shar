package flag

const (
	Server        = "server"
	LogLevel      = "loglevel"
	ServerShort   = "s"
	LogLevelShort = "l"
)

type Set struct {
	Server   string
	LogLevel string
}

var Value Set
