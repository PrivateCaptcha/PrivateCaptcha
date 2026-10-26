//go:build !enterprise

package widget

import "embed"

const enterpriseBuild = false

//go:embed static
var staticFiles embed.FS
