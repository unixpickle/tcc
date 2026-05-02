package tcc

import (
	"io"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

func parseZones(r io.Reader) ([]Zone, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	var zones []Zone
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" && hasClass(n, "gray-capsule") {
			if zone, ok := parseZoneRow(n); ok {
				zones = append(zones, zone)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return zones, nil
}

func parseZoneRow(n *html.Node) (Zone, bool) {
	var zone Zone
	id, ok := attr(n, "data-id")
	if !ok {
		return Zone{}, false
	}
	parsedID, err := strconv.Atoi(id)
	if err != nil {
		return Zone{}, false
	}
	zone.ID = parsedID
	if controlURL, ok := attr(n, "data-url"); ok {
		zone.ControlURL = absoluteURL(controlURL)
	}
	var walk func(*html.Node)
	walk = func(child *html.Node) {
		switch {
		case child.Type == html.ElementNode && hasClass(child, "location-name"):
			zone.Name = strings.TrimSpace(nodeText(child))
		case child.Type == html.ElementNode && hasClass(child, "tempValue"):
			if value, ok := parseNumber(nodeText(child)); ok {
				zone.Temperature = &value
			}
		case child.Type == html.ElementNode && hasClass(child, "hum-num"):
			if value, ok := parseNumber(nodeText(child)); ok {
				zone.Humidity = &value
			}
		}
		for grandchild := child.FirstChild; grandchild != nil; grandchild = grandchild.NextSibling {
			walk(grandchild)
		}
	}
	walk(n)
	return zone, true
}

func attr(n *html.Node, key string) (string, bool) {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val, true
		}
	}
	return "", false
}

func hasClass(n *html.Node, class string) bool {
	value, ok := attr(n, "class")
	if !ok {
		return false
	}
	for _, part := range strings.Fields(value) {
		if part == class {
			return true
		}
	}
	return false
}

func nodeText(n *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(child *html.Node) {
		if child.Type == html.TextNode {
			builder.WriteString(child.Data)
		}
		for grandchild := child.FirstChild; grandchild != nil; grandchild = grandchild.NextSibling {
			walk(grandchild)
		}
	}
	walk(n)
	return builder.String()
}

func parseNumber(value string) (float64, bool) {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.TrimSuffix(cleaned, "°")
	cleaned = strings.TrimSuffix(cleaned, "%")
	cleaned = strings.TrimSpace(cleaned)
	result, err := strconv.ParseFloat(cleaned, 64)
	return result, err == nil
}

func absoluteURL(value string) string {
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	if strings.HasPrefix(value, "/") {
		return baseURL + value
	}
	return baseURL + "/" + value
}

func locationIDFromZonesURL(value *url.URL) int {
	parts := strings.Split(strings.Trim(value.Path, "/"), "/")
	for i, part := range parts {
		if part == "portal" && i+2 < len(parts) && parts[i+2] == "Zones" {
			locationID, _ := strconv.Atoi(parts[i+1])
			return locationID
		}
	}
	return 0
}
