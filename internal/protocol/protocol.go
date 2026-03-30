package protocol

type Request struct {
	Command string `json:"command"`
	Name    string `json:"name,omitempty"`
	Offset  int    `json:"offset,omitempty"`
	Input   string `json:"input,omitempty"` // for "input" command - data to send to process stdin
}

type Response struct {
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	TotalLines int    `json:"total_lines,omitempty"`
	Layout     *Layout `json:"layout,omitempty"`
}

// Layout describes the tab/pane structure for the TUI
type Layout struct {
	Tabs []TabLayout `json:"tabs"`
}

type TabLayout struct {
	Name   string       `json:"name"`
	Layout string       `json:"layout"` // "vertical", "horizontal", "split"
	Panes  []PaneLayout `json:"panes"`
}

type PaneLayout struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Status  string `json:"status"`
}
