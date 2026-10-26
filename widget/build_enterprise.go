//go:build enterprise

package widget

import "embed"

const enterpriseBuild = true

//go:embed static whitelabel
var staticFiles embed.FS
