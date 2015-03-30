package main

import (
	"fmt"
	"html/template"
	"log"
	"strings"
	"time"
)

func helperMap() template.FuncMap {
	return template.FuncMap{
		"truncate":        truncate,
		"add":             add,
		"eq":              eq,
		"labelsToStrings": labelsToStrings,
		"sanitizeLabels":  sanitizeLabels,
		"fields":          strings.Fields,
		"bytesToHtml":     bytesToHtml,
		"uriEncode":       uriEncode,
		"strEq":           strEq,
		"join":            strings.Join,
		"summaryTail":     summaryTail,
		"omissionCount":   omissionCount,
	}
}

func truncate(charCount int, s string) string {
	idx := strings.LastIndex(s, ".")
	if idx == -1 || idx < charCount {
		return s
	}

	return s[:charCount] + "(...)" + s[idx:]
}

func sanitizeLabels(labels []string) []string {
	out := []string{}
	for _, x := range labels {
		noOperators := strings.TrimLeft(x, "-+")

		if strings.HasPrefix(noOperators, "!!_") ||
			strings.HasPrefix(noOperators, "!?_") {
			continue
		}

		out = append(out, x)
	}
	return out
}

func uriEncode(s string) string {
	const reserved = "!#$&'()*+,/:;=?@[]"
	out := ""
	for _, c := range s {
		if strings.Contains(reserved, string(c)) {
			out = strings.Join([]string{out, fmt.Sprintf("%%%X", c)}, "")
		} else {
			out = strings.Join([]string{out, string(c)}, "")
		}
	}

	return out
}

func add(x, y int) int { return x + y }
func eq(x, y int) bool { return x == y }

func strEq(s1, s2 string) bool {
	log.Printf("[%s] [%s]", s1, s2)
	return s1 == s2
}

func bytesToHtml(bs []byte) template.HTML {
	return template.HTML(bs)
}

func stringToHtml(s string) template.HTML {
	return template.HTML(s)
}

func siteName() string {
	return settings.General.SiteName
}

func makeTimestamp(unixSeconds int) string {
	when := time.Unix(int64(unixSeconds), 0)
	timestamp := when.Format(settings.General.PostTimeFormat)
	return timestamp
}

func omissionCount(t *thread) contentCount {
	var total, media, image, video, audio int
	shown := append([]*post{t.Posts[0]}, summaryTail(t)...)

	for _, p := range shown {
		total++

		if p.Media == nil {
			continue
		}

		media++

		switch p.Media.MediaType {
		case "image":
			image++
		case "audio":
			audio++
		case "video":
			video++
		}
	}

	return contentCount{
		Posts: t.Count.Posts - total,
		Media: t.Count.Media - media,
		Image: t.Count.Image - image,
		Video: t.Count.Video - video,
		Audio: t.Count.Audio - audio,
	}
}

func summaryTail(t *thread) []*post {
	out := []*post{}
	start := len(t.Posts) - settings.General.SummaryPostTailLength

	if start < 1 {
		start = 1
	}

	for _, p := range t.Posts[start:len(t.Posts)] {
		out = append(out, p)
	}

	return out
}

func days(unixSeconds int) string {
	return fmt.Sprintf("%d day(s)", unixSeconds/60/60/24)
}
