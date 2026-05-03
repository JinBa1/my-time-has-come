package version

import "fmt"

var (
	Version   = "v0-dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type Info struct {
	Version   string
	Commit    string
	BuildDate string
}

func Current() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
	}
}

func (i Info) Format() string {
	return fmt.Sprintf("mthc %s\ncommit: %s\nbuilt: %s\n", i.Version, i.Commit, i.BuildDate)
}
