package flag

const (
	CorrelationKey      = "correlationKey"
	Server              = "server"
	LogLevel            = "loglevel"
	CorrelationKeyShort = "k"
	ServerShort         = "s"
	LogLevelShort       = "l"
)

type Set struct {
	Server         string
	LogLevel       string
	CorrelationKey string
}

var Value Set
