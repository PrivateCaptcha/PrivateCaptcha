package owl

const DefaultTagName = "owl"

var tagName string = DefaultTagName

// UseTag sets the tag name to parse directives.
func UseTag(tag string) {
	tagName = tag
}

// Tag returns the tag name where the directives are parsed from.
func Tag() string {
	return tagName
}
