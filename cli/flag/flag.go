package flag

const (
	CorrelationKey      = "correlationKey"
	Server              = "server"
	LogLevel            = "loglevel"
	Vars				= "vars"
	VarsShort			= "v"
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
	Vars		   []string
}

var Value Set
