package portal

// ViewPortalPage describes a single renderable portal page for the viewportal tool.
type ViewPortalPage struct {
	Path       string
	Template   string
	ShowInList bool
	ModelFunc  func(alert AlertRenderContext) interface{}
}
