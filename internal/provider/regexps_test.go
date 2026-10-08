package provider

import "regexp"

var (
	regexpPlanoApagado = regexp.MustCompile(`would delete an existing plan`)
	regexpV1           = regexp.MustCompile(`only accepts "2\.0\.0"`)
)
