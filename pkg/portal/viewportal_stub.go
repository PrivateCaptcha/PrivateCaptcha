//go:build !viewportal

package portal

// BuildViewPortalPages returns stub portal pages.
// This is a no-op stub; the real implementation is in viewportal.go (viewportal build tag).
func (s *Server) BuildViewPortalPages() []ViewPortalPage {
	return nil
}
