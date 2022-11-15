package flag

const (
	CorrelationKey      = "correlationKey" // Name of the correlation key flag.
	Server              = "server"         // Name of the server flag.
	LogLevel            = "loglevel"       // Name of the log level flag.
	Vars                = "vars"           // Name of the vars flag.
	VarsShort           = "v"              // Short name of the vars flag.
	CorrelationKeyShort = "k"              // Short name of the correlation key flag.
	ServerShort         = "s"              // Short name of the server flag.
	LogLevelShort       = "l"              // Short name of the log level flag.
	DebugTrace          = "debug"          // Name of the debug trace flag.
	DebugTraceShort     = "d"              // Short name of the debug trace flag.
)

type Set struct {
	Server         string   // NATS server URL setting
	LogLevel       string   // Log level setting
	CorrelationKey string   // Correlation key setting.
	DebugTrace     bool     // Debug trace enabled setting.
	Vars           []string // Workflow variables setting.
}

var Value Set // Runtime vales for flags
