package flag

const (
	CorrelationKey      = "correlationKey"
	Server              = "server"
	LogLevel            = "loglevel"
	CorrelationKeyShort = "k"
	ServerShort         = "s"
	LogLevelShort       = "l"
	DebugTrace			= "debug"
	DebugTraceShort		= "d"
)

type Set struct {
	Server         string
	LogLevel       string
	CorrelationKey string
	DebugTrace	   bool
}

var Value Set
