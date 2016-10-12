package main

import (
	"golang.org/x/net/websocket"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func storeImage(r *http.Request) (string, *media, error) {
	file, header, e := r.FormFile("upload")
	if e != nil {
		return "", nil, nil
	}
	fileFormat := header.Header.Get("Content-Type")

	if !isValidFormat(fileFormat) {
		return "", nil, errors.New("invalid_format")
	}
	i, e := mediaStore.dispatch(file, fileFormat)
	return header.Filename, i, e
}

func isValidFormat(fileFormat string) bool {
	for _, acceptable := range settings.Image.AcceptedFileFormats {
		if fileFormat == acceptable {
			return true
		}
	}

	for _, acceptable := range settings.Video.AcceptedFileFormats {
		if fileFormat == acceptable {
			return true
		}
	}

	for _, acceptable := range settings.Audio.AcceptedFileFormats {
		if fileFormat == acceptable {
			return true
		}
	}

	return false
}

func showThread(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitAfterN(r.URL.Path, "/", 3)
	if len(parts) < 3 {
		msg(w, 404, "404")
		return
	}

	tid := string(threadId(parts[2]))
	if !pageCache.Get(tid, w, r) {
		msg(w, 404, "404")
		return
	}
}

func postThread(w http.ResponseWriter, r *http.Request) {
	ip := strings.SplitN(r.RemoteAddr, ":", 2)[0]

	if !siteUsers.InThreshold(ip, "NewThread") {
		msg(w, http.StatusOK, "too_many_threads")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, settings.General.maxFileSize)
	r.ParseMultipartForm(settings.General.maxFileSize)

	if normalizePostFields(r) != nil {
		msg(w, http.StatusOK, "invalid_fields")
		return
	}

	tags := strings.Fields(r.Form.Get("tag_entry"))

	switch r.Form.Get("nsfw") {
	case "true":
		tags = append(tags, "!!_nsfw")
	case "":
		msg(w, http.StatusOK, "must_indicate_nsfw")
		return
	}

	recordPost(w, r, &post{OP: true, Tags: tags})
}

func postComment(w http.ResponseWriter, r *http.Request) {
	ip := strings.SplitN(r.RemoteAddr, ":", 2)[0]

	if !siteUsers.InThreshold(ip, "NewPost") {
		msg(w, http.StatusOK, "too_many_posts")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, settings.General.maxFileSize)
	r.ParseMultipartForm(settings.General.maxFileSize)

	if normalizePostFields(r) != nil {
		msg(w, http.StatusOK, "invalid_fields")
		return
	}

	tid := threadId(r.Form.Get("thread_no"))
	recordPost(w, r, &post{ParentThread: tid})
}

func recordPost(w http.ResponseWriter, r *http.Request, p *post) {
	if p.Role = getStaffRole(r); p.Role.Title != "" {
		staff, _ := getStaff(r)
		p.RoleName = staff.Role
	}

	if r.Form.Get("post_with_role") == "on" {
		if !p.Role.PostWithRole {
			msg(w, http.StatusOK, "unauthorized")
			return
		}
		p.ShowRole = true
	}

	ip := strings.SplitN(r.RemoteAddr, ":", 2)[0]

	imgName, img, e := storeImage(r)
	if img == nil && p.OP {
		msg(w, http.StatusOK, "no_image")
		return
	} else if e != nil {
		msg(w, http.StatusOK, "image_decode_failure")
		return
	} else if img != nil && img.Blocked != nil {
		log.Println("image was blocked")
		if img.Blocked.Name != "no_ban" {
			log.Println("issuing ban for posting blocked image.")
			siteUsers.IssueBan(ip, *img.Blocked)
		}

		msg(w, http.StatusOK, "image_decode_failure")
		return
	}

	uid, e := strconv.ParseUint(r.Form.Get("user_num"), 10, 64)
	if e != nil {
		msg(w, http.StatusNotFound, "unable_to_post")
		return
	}

	target, e := strconv.ParseUint(r.Form.Get("reply_to"), 10, 64)
	if e == nil {
		p.ReplyTo = postLid(target)
	}

	p.Comment = r.Form.Get("comment")
	if p.Comment == "" && img == nil {
		msg(w, http.StatusNotFound, "need_pic_or_text")
		return
	}

	p.UserAddr = ip
	p.Media = img
	p.MediaName = imgName
	p.DesiredUserId = uid

	hiveReq(func(h *hive) {
		pr, err := h.AddPost(p)
		if err != nil {
			msg(w, http.StatusOK, err.Error())
			return
		}

		// Don't redirect xmlHttpRequest.
		if r.Form.Get("no_redirect") == "" {
			threadUrl := fmt.Sprintf("t/%s#%d", pr.Thread, pr.Local)
			http.Redirect(w, r, threadUrl, http.StatusFound)
		}
	})
}

func showFullMedia(w http.ResponseWriter, r *http.Request) {
	if !checkReferer(r) {
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		return
	}

	hash := parts[2]
	vals := "max-age=360, public, must-revalidate, proxy-revalidate"
	w.Header().Add("Cache-Control", vals)
	mediaStore.WriteMedia(w, hash, true)
}

func showRobots(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "User-agent: *\nDisallow: /")
}

func showThumbImage(w http.ResponseWriter, r *http.Request) {
	if !checkReferer(r) {
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	hash := parts[2]

	vals := "max-age=315360000, public, must-revalidate, proxy-revalidate"
	w.Header().Add("Cache-Control", vals)
	mediaStore.WriteMedia(w, hash, false)
}

// Registers the websocket under the thread's listeners. When
// the handler exits the websocket is automatically closed, so
// to stop this the handler blocks on a channel waiting for
// notification that the thread is no longer broadcasting.
func threadUpdater(ws *websocket.Conn) {
	r := ws.Request()
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) > 3 {
		return
	}

	reg := wsRegistration{threadId(parts[2]), make(chan bool, 1), ws}
	regListener <- reg
	<-reg.Done
}

func showTagQuery(page int, query, view string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		terms := strings.Fields(query)

		var search parsedQuery
		search.QueryString = query
		search.Page = page
		search.WriteOut = w
		search.View = view

		if getStaffRole(r).ViewRestrictedTags {
			search.Admin = true
		}

		isBadAdminTagQuery := func(term string) bool {
			return strings.HasPrefix(term, "!?_") && !search.Admin
		}

		for _, term := range terms {
			switch term[0] {
			case '-':
				if len(term) > 1 && !isBadAdminTagQuery(term[1:]) {
					search.Exclude = append(search.Exclude, term[1:])
				}
			case '+':
				if len(term) > 1 && !isBadAdminTagQuery(term[1:]) {
					search.Merge = append(search.Merge, term[1:])
				}
			default:
				if !isBadAdminTagQuery(term) {
					search.Filter = append(search.Filter, term)
				}
			}
		}

		nsfw, e := r.Cookie("show_nsfw")
		if e != nil || nsfw.Value != "true" {
			search.Exclude = append(search.Exclude, "!!_nsfw")
		}

		hiveReq(func(h *hive) {
			h.TagQuery(search)
		})
	}
}

func pageRange(center, count int) []int {
	pages := count / (settings.Catalog.ThreadsPerPage + 1)

	start := center - (settings.Catalog.PageRange / 2)
	if start < 0 {
		start = 0
	}

	end := start + settings.Catalog.PageRange
	if end > pages {
		end = pages
	}

	out := []int{}
	for i := 0; i <= end; i++ {
		out = append(out, start+i)
	}
	return out
}

func showCatQueryFromURI(w http.ResponseWriter, r *http.Request) {
	doQuery(w, r, "catalog")
}

func showSumQueryFromURI(w http.ResponseWriter, r *http.Request) {
	doQuery(w, r, "summary")
}

func doQuery(w http.ResponseWriter, r *http.Request, view string) {
	if page, query := readQueryParts(w, r); query != "" {
		showTagQuery(page, query, view)(w, r)
	} else {
		msg(w, 404, "404")
	}
}

func readQueryParts(w http.ResponseWriter, r *http.Request) (int, string) {
	parts := strings.Split(r.URL.Path, "/")
	length := len(parts)

	var page int
	var e error
	if length >= 4 {
		page, e = strconv.Atoi(parts[3])
		if e != nil {
			return 0, ""
		}
	} else if length == 3 {
		page = 0
	} else if length < 3 {
		return 0, ""
	}

	r.ParseForm()
	return page, parts[2]
}

func showCatQuery(w http.ResponseWriter, r *http.Request) {
	showQuery(w, r, "cat")
}

func showSumQuery(w http.ResponseWriter, r *http.Request) {
	showQuery(w, r, "sum")
}

func showQuery(w http.ResponseWriter, r *http.Request, view string) {
	r.ParseForm()
	queryString := r.Form.Get("query")

	if queryString == "" {
		http.Redirect(w, r, "/tag/!!_all", http.StatusFound)
		return
	}

	query, e := url.Parse(queryString)
	if e != nil {
		msg(w, 404, "404")
		return
	}

	http.Redirect(w, r, "/"+view+"/"+
		uriEncode(query.String()), http.StatusFound)
}

func showReportLanding(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		msg(w, 200, "invalid_fields")
		return
	}

	lid, e := strconv.Atoi(parts[4])
	if e != nil {
		msg(w, 200, "invalid_fields")
		return
	}

	gid, e := strconv.Atoi(parts[2])
	if e != nil {
		msg(w, 200, "invalid_fields")
		return
	}

	ref := &postRef{
		Thread: threadId(parts[3]),
		Global: postGid(gid),
		Local:  postLid(lid),
	}

	if e := templates.ExecuteTemplate(w, "report_landing", ref); e != nil {
		log.Println(e)
	}
}

func postReport(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	ip := strings.SplitN(r.RemoteAddr, ":", 2)[0]
	reason := r.Form.Get("report_reason")
	tid := threadId(r.Form.Get("tid"))
	gid, e := strconv.Atoi(r.Form.Get("gid"))

	if e != nil {
		msg(w, 200, "invalid_fields")
		return
	}

	if !siteUsers.InThreshold(ip, "ReportPost") {
		msg(w, http.StatusOK, "too_many_reports")
		return
	}

	hiveReq(func(h *hive) {
		e = h.ReportPost(postGid(gid), reason, ip)
	})

	if e != nil {
		msg(w, 200, e.Error())
	} else {
		target := fmt.Sprintf("/t/%s", tid)
		passthrough(w, "post_reported", target)
	}
}

func getAutocomplete(w http.ResponseWriter, r *http.Request) {
	tagSearch.WriteList(w)
}

func msg(w http.ResponseWriter, status int, msgName string) {
	w.WriteHeader(status)
	templates.ExecuteTemplate(w, msgName, nil)
}

func passthrough(w http.ResponseWriter, msgName string, url string) {
	type data struct {
		MsgName string
		Target  string
		Delay   int
	}

	delay := int(settings.General.PassthroughDelay.Duration.Seconds())
	e := templates.ExecuteTemplate(w, "passthrough",
		data{msgName, url, delay})
	if e != nil {
		log.Println(e)
	}
}

func labelsToStrings(labels []string) []string {
	out := []string{}
	for _, x := range labels {
		out = append(out, x)
	}
	return out
}

func checkReferer(r *http.Request) bool {
	refAddr, e := url.Parse(r.Referer())
	if e != nil {
		return false
	}

	for _, valid := range settings.Media.ValidReferers {
		if refAddr.Host == valid {
			return true
		}
	}

	log.Println("%s requested %s with invalid http referer: %s",
		r.RemoteAddr, r.RequestURI, refAddr.Host)

	return false
}

func checkWordFilters(text string) (flagged bool, ban *banReason) {
	for _, filter := range settings.WordFilters {
		if filter.Regexp.MatchString(text) {
			ban, ok := settings.BanReasons[filter.Ban]
			if !ok {
				panic("checkWordFilters: invalid ban reason: " + filter.Ban)
			}
			return true, &ban
		}
	}

	return false, nil
}
