package policy

import "time"

type Decision int

const (
	NoAction Decision = iota
	SoftInject
	HardStop
)

type State struct{}
type Config struct{}

func Decide(state State, config Config, now time.Time) Decision {
	return NoAction
}
