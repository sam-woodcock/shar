package flag

const (
	Server      = "server"
	ServerShort = "s"
)

type FlagSet struct {
	Server string
}

var Value FlagSet
