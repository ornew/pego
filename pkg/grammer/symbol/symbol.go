package symbol

import (
	"encoding/json"
	"fmt"
)

type NonTerminal struct {
	Name string `json:"name"`
}

func (s *NonTerminal) String() string {
	return s.Name
}

type Terminal struct {
	Text string `json:"text,omitempty"`
}

func (s *Terminal) String() string {
	b, _ := json.Marshal(s.Text)
	return string(b)
}

type TerminalRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func (s *TerminalRange) String() string {
	return fmt.Sprintf("[%s-%s]", s.Start, s.End)
}
