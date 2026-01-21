package models

// Metadata about the stream event
type Meta struct {
	URI       string `json:"uri"`
	RequestID string `json:"request_id"`
	ID        string `json:"id"`
	DT        string `json:"dt"`
	Domain    string `json:"domain"`
	Stream    string `json:"stream"`
	Topic     string `json:"topic"`
	Partition int    `json:"partition"`
	Offset    int64  `json:"offset"`
}

// Length of the old article before revision vs after revision in bytes
type Length struct {
	Old int `json:"old"`
	New int `json:"new"`
}

// Revision IDs
type Revision struct {
	Old int `json:"old"`
	New int `json:"new"`
}

// Data about a recent Wikimedia change
type Message struct {
	Schema           string    `json:"$schema"`
	Meta             Meta      `json:"meta"`
	ID               int64     `json:"id"`
	Type             string    `json:"type"`
	Namespace        int       `json:"namespace"`
	Title            string    `json:"title"`
	TitleURL         string    `json:"title_url"`
	Comment          string    `json:"comment"`
	Timestamp        int64     `json:"timestamp"`
	User             string    `json:"user"`
	Bot              bool      `json:"bot"`
	NotifyURL        string    `json:"notify_url"`
	ServerURL        string    `json:"server_url"`
	ServerName       string    `json:"server_name"`
	ServerScriptPath string    `json:"server_script_path"`
	Wiki             string    `json:"wiki"`
	ParsedComment    string    `json:"parsedcomment"`
	Minor            *bool     `json:"minor,omitempty"`
	Length           *Length   `json:"length,omitempty"`
	Revision         *Revision `json:"revision,omitempty"`
}
