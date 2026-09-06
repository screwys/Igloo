package xfeed

import (
	"strings"

	"golang.org/x/net/html"
)

// Article HTML comes from gallery-dl. Store readable text and let the existing
// media pipeline cache its separate image/video records; never render remote HTML.
func articleFromMeta(meta map[string]any) (string, string) {
	article, _ := meta["article"].(map[string]any)
	if article == nil {
		return "", ""
	}
	title := strings.TrimSpace(firstString(article, "title"))
	doc, err := html.Parse(strings.NewReader(firstString(article, "html")))
	if err != nil {
		return "", ""
	}
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "head", "img", "video", "iframe":
				return
			case "br":
				text.WriteByte('\n')
				return
			}
		}
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "blockquote", "pre":
				text.WriteString("\n\n")
			case "a":
				for _, attr := range n.Attr {
					if attr.Key == "href" && (strings.HasPrefix(attr.Val, "https://") || strings.HasPrefix(attr.Val, "http://")) {
						text.WriteString(" (" + attr.Val + ")")
					}
				}
			}
		}
	}
	walk(doc)
	var paragraphs []string
	for _, paragraph := range strings.Split(text.String(), "\n\n") {
		if paragraph = strings.TrimSpace(paragraph); paragraph != "" {
			paragraphs = append(paragraphs, paragraph)
		}
	}
	if len(paragraphs) == 0 {
		return "", ""
	}
	return title, strings.Join(paragraphs, "\n\n")
}
