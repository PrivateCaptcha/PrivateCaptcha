package portal

// ViewPortalPage describes a single renderable portal page for the viewportal tool.
type ViewPortalPage struct {
	Path           string
	Template       string
	ParentTemplate string // if set, cmd/viewportal renders with parent instead of Template
	ModelFunc      func(alert AlertRenderContext) interface{}
}
