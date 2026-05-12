package model

// DownloaderOperation is a compact, redacted summary of one external or direct
// downloader operation. It intentionally omits raw command output, full command
// arguments, and credential material.
type DownloaderOperation struct {
	ID          int64  `json:"id"`
	Operation   string `json:"operation"`
	Platform    string `json:"platform"`
	Subject     string `json:"subject"`
	Tool        string `json:"tool"`
	StartedAtMs int64  `json:"started_at_ms"`
	EndedAtMs   int64  `json:"ended_at_ms"`
	Status      string `json:"status"`
	ErrorKind   string `json:"error_kind"`
	Error       string `json:"error"`
	CookieLabel string `json:"cookie_label"`
	ElapsedMs   int64  `json:"elapsed_ms"`
	ItemCount   int    `json:"item_count"`
	MediaCount  int    `json:"media_count"`
	FileCount   int    `json:"file_count"`
	Bytes       int64  `json:"bytes"`
	SummaryJSON string `json:"summary_json"`
}
