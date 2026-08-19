package projector

import "net/url"

// IsOpenableURL reports whether rawURL is safe to present as a clickable
// link: it must parse, use the http or https scheme, and have a non-empty
// host. It never modifies, canonicalizes, or dereferences the URL.
func IsOpenableURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}
