package jumpaccess

import _ "embed"

//go:embed LICENSE
var projectLicense string

//go:embed THIRD-PARTY-NOTICES.txt
var thirdPartyNotices string

// Licenses returns the project license followed by all notices required for
// third-party software compiled into jumpctl.
func Licenses() string {
	return projectLicense + "\n" + thirdPartyNotices
}
