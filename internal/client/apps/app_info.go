package apps

import "time"

type AppID = string
type AppSource = string

const (
	AppSourceGit      AppSource = "git"
	AppSourceLocalDir AppSource = "local"
)

type AppInfo struct {
	ID          AppID     `json:"id"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Source      AppSource `json:"source"`
	SourceURI   string    `json:"sourceURI,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	Tag         string    `json:"tag,omitempty"`
	Commit      string    `json:"commit,omitempty"`
	InstalledOn time.Time `json:"installedOn,omitempty"`
}
