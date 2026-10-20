package apicontract

type SystemSFTP struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Path           string `json:"path"`
	HostKey        string `json:"hostKey"`
	Timeout        int    `json:"timeout"`
	ConcurrentReqs int    `json:"concurrentReqs"`
}
