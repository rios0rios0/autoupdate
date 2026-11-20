package repositories

// OriginRepository answers what pushing to a checkout's origin involves, which `autoupdate .`
// has to know before it changes anything: whether the push needs a token, and which server
// origin is on.
type OriginRepository interface {
	// PushURLs returns every URL a push to origin may dial, as go-git reads them: each `url`
	// and then each `pushurl`, from the repository's own configuration with only its own
	// insteadOf rules applied. go-git pushes to the last one. A checkout whose configuration
	// go-git cannot read is an error, because the push would fail on it too.
	PushURLs(repoDir string) ([]string, error)

	// HostName returns the server a remote URL reaches: its host, or, for an SSH URL whose
	// host is an alias, the HostName the operator's SSH client configuration gives it.
	HostName(remoteURL string) string
}
