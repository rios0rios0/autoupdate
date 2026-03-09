package pipeline

// TruncateToGranularity is exported for testing.
func TruncateToGranularity(latest, reference string) string {
	return truncateToGranularity(latest, reference)
}

// IsExactVersion is exported for testing.
func IsExactVersion(ver string) bool {
	return isExactVersion(ver)
}
