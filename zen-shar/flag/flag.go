package flag

const (
	Server        = "server"   // Server is the name of the server flag.
	LogLevel      = "loglevel" // LogLevel is the name of the log level flag.
	ServerShort   = "s"        // ServerShort is the short name of the server flag.
	LogLevelShort = "l"        // LogLevelShort is the short name of the log level flag.
)

// Set is a container for all of the flag messages used by zen-shar
type Set struct {
	Server         string   // Server is the NATS server URL setting
	LogLevel       string   // LogLevel is the Log level setting
	CorrelationKey string   // CorrelationKey is the correlation key setting.
	DebugTrace     bool     // DebugTrace is the debug trace enabled setting.
	Vars           []string // Vars is the workflow variables setting.
}

var Value Set // Value contains the runtime values for flags
