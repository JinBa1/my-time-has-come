package state

type State struct{}

func Load(path string) (*State, error) {
	return &State{}, nil
}

func (s *State) Write(path string) error {
	return nil
}
