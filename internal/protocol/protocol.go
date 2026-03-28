package protocol

type Request struct {
	Command string `json:"command"`
	Name    string `json:"name,omitempty"`
}

type Response struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
