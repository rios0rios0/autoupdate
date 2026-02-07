package config

//nolint:gochecknoglobals // required to export unexported functions for black-box testing
var (
	ResolveToken = resolveToken
	Validate     = validate
)
