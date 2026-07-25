package reserve

import _ "embed"

//go:embed reserve.lua
var ReserveScriptSrc string

//go:embed commit.lua
var CommitScriptSrc string

//go:embed release.lua
var ReleaseScriptSrc string
