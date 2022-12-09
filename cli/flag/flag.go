package flag

const (
	// CorrelationKey is the flag name for the correlation key for a BPMN message.
	CorrelationKey = "correlationKey"
	// Server is the fhe flag name for the server address.
	Server = "server"
	// LogLevel is the flag name for the logging level.
	LogLevel = "loglevel"
	// Vars is the flag name for variables passed to the workflow engine.
	Vars = "vars"
	// VarsShort is the flag name for variables passed to the workflow engine.
	VarsShort = "v"
	// CorrelationKeyShort is the short flag name for the correlation key for a BPMN message.
	CorrelationKeyShort = "k"
	// ServerShort is the the short flag name for the server address.
	ServerShort = "s"
	// LogLevelShort is the short flag name for the logging level.
	LogLevelShort = "l"
	// DebugTrace is the short flag name for the trace switch.
	DebugTrace = "debug"
	// DebugTraceShort is the short flag name for the trace switch.
	DebugTraceShort = "d"
	// JsonOutput is the flag name for machine readable json responses.
	JsonOutput = "json"
	// JsonOutputShort is the short flag name for machine readable json responses.
	JsonOutputShort = "j"
)

// Set is the set of flags associated with the CLI.
type Set struct {
	Server         string
	LogLevel       string
	CorrelationKey string
	DebugTrace     bool
	Vars           []string
	Json           bool
}

// Value contains the values of the SHAR CLI flags.
var Value Set
