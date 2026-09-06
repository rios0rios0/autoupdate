package support

import (
	logger "github.com/sirupsen/logrus"
)

// VersionFeed names one updater's release feed for the two lines the lookup
// logs. The wording differs between updaters because the feeds do: one reports
// the newest stable release, another the newest LTS.
type VersionFeed struct {
	// LogPrefix tags the log lines, e.g. "python".
	LogPrefix string
	// Runtime names the runtime the lookup failed for, e.g. "Python".
	Runtime string
	// Release describes what the feed reports, e.g. "stable Python".
	Release string
}

// LatestVersion reports the release the feed named, or the empty string when
// the feed could not be reached.
//
// A feed that is momentarily unreachable must not fail the run: refreshing the
// dependencies is still worth doing, it just goes ahead without moving the
// runtime pin -- which is what the empty string means to every caller.
func LatestVersion(feed VersionFeed, fetch func() (string, error)) string {
	version, err := fetch()
	if err != nil {
		logger.Warnf(
			"[%s] Failed to fetch latest %s version: %v (continuing without version upgrade)",
			feed.LogPrefix, feed.Runtime, err,
		)

		return ""
	}

	logger.Infof("[%s] Latest %s version: %s", feed.LogPrefix, feed.Release, version)

	return version
}
