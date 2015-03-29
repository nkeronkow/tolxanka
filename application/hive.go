package main

import (
    "bytes"
    "errors"
    "math/rand"
    "strconv"
    "strings"
    "time"
    "html/template"
    "log"
)

type hive struct {
    Threads         map[threadId]*thread
    Posts           map[postGid]*post
    ThreadCount     uint
    PostCount       uint
    AggregateQueue  *thread
    IllegalQueue    *thread
    RuleQueue       *thread
    tags            tagMap
    escaper         func(string)string
    ThreadFields    []fieldNames
    ThreadForm      template.HTML
    ThreadFormGenTime   time.Time
}

func newHive() *hive {
    h := &hive{
        Threads:    map[threadId]*thread{},
        Posts:      map[postGid]*post{},
        escaper:    genMarkup(),
        tags:       map[string]*tag{},
    }
    h.UpdateThreadForm()
    h.createReportQueues()
    return h
}

func (h *hive) createReportQueues() {
    createQueue := func(comment string) *thread {
        p := &post{
            Comment:        comment,
            Tags:           []string{"!?_admin"},
            OP:             true,
            NoDump:         true,
            Role:           settings.Roles["Bot"],
            ShowRole:       true,
        }

        if _, e := h.AddPost(p); e != nil {
            panic(e)
        }

        t, ok := h.Threads[p.ParentThread]
        if !ok {
            log.Fatal("Could not create report queues")
        }

        // Remove queues from !!_all to avoid them getting pruned.
        all, _ := h.tags["!!_all"]
        all.Normal.RemoveThread(t)

        t.Hidden = true
        return t
    }

    h.AggregateQueue = createQueue("Aggregate report queue")
    h.IllegalQueue = createQueue("Illegal content report queue")
    h.RuleQueue = createQueue("General rule violation content report queue")
}


func constrainPost(t *thread, p *post) error {
    if t.Locked && !p.Recovered && !p.Role.PostInLockedThread {
        return errors.New("thread_locked")
    }

    if len(p.Comment) > settings.Limit.CommentLength {
        return errors.New("comment_too_long")
    }

    if strings.Count(p.Comment, "\n") > settings.Limit.NewlinesPerPost {
        return errors.New("comment_too_long")
    }

    if flagged, ban := checkWordFilters(p.Comment); flagged {
        if ban != nil {
            siteUsers.IssueBan(p.UserAddr, *ban)
        }
        return errors.New("word_filter_blocked")
    }

    return nil
}

func (h *hive) AddPost(p *post) (postRef, error) {
    var t *thread
    var e error

    if p.OP {
        t, e = h.newThreadFromPost(p)
        if e != nil {
            return postRef{}, e
        }
    }

    t, ok := h.Threads[p.ParentThread]
    if !ok {
        return postRef{}, errors.New("thread_not_exist")
    }

    if e := constrainPost(t, p); e != nil {
        return postRef{}, e
    }

    if !p.ShowRole {
        p.Role = Role{}
    }

    h.PostCount++
    p.GlobalId = postGid(h.PostCount)
    p.EscapedComment = template.HTML(h.escaper(p.Comment))
    p.EscapedImageName = template.HTML(h.escaper(p.MediaName))
    t.AddPost(p)

    if !p.Recovered && !p.NoDump {
        persistPost <-p
    }

    for _, label := range t.Tags {
        tag, ok := h.tags[label]
        if ok {
            tag.Bump(t)
        }
    }

    if !p.Recovered && t.Count.Posts >= settings.Limit.PostsPerThread {
        lockPost := &post{
            Comment:        "Reply limit reached. Thread locked.",
            Role:           settings.Roles["Bot"],
        }
        lockPost.EscapedComment = template.HTML(h.escaper(lockPost.Comment))
        t.AddPost(lockPost)
        lockThread(t.Id, true)
    }

    h.Posts[p.GlobalId] = p
    return postRef{0, p.LocalId, t.Id}, nil
}

func (h *hive) ReportPost(gid postGid, reason, ip string) error {
    p := h.GetPost(gid)
    if p == nil {
        return errors.New("post_not_exist")
    }

    if _, ok := p.ReportedBy[ip]; ok {
        return errors.New("already_reported")
    }
    p.ReportedBy[ip] = true

    queues := []*thread{ h.AggregateQueue }
    switch reason {
    case "rule_violation":
        queues = append(queues, h.RuleQueue)
    case "illegal":
        timesReported := len(p.ReportHistory[h.IllegalQueue.Id]) + 1
        log.Printf("times reported: %v", timesReported)

        if timesReported == 1 {
            go mailNotify(ip, p)
        } else if timesReported >= settings.General.AutoDeleteThreshold {
            h.HidePost(p)
        }

        queues = append(queues, h.IllegalQueue)
    }

    userReport := report{ ip, time.Now() }
    for _, q := range queues {
        q.AddPostToReportQueue(p)
        p.ReportHistory[q.Id] = append(p.ReportHistory[q.Id], userReport)
    }

    return nil
}


func (h *hive) TagQuery (search parsedQuery) {
    page := search.Page
    normal, n := h.tags.Query(
                    page * settings.Catalog.ThreadsPerPage,
                    (page+1) * settings.Catalog.ThreadsPerPage,
                    search)

    sticky := []*thread{}
    if page == 0 {
        sticky = h.tags.GetStickyThreads(append(search.Merge, search.Filter...))
    }

    h.assembleResultsPage(normal, sticky, n, page, search)
}

func (h *hive) newThreadFromPost(p *post) (*thread, error) {
    var e error
    p.Tags, e = cleanUserTags(p)
    if e != nil {
        return nil, e
    }

    t, e := h.buildNewThread(p.Tags)
    if e != nil {
        return nil, e
    }

    p.ParentThread = t.Id
    t.NoDump = p.NoDump

    if !t.NoDump {
        persistThread <-t
    }

    return t, e
}

func (h *hive) buildNewThread(tags []string) (*thread, error) {
    if len(tags) == 0 {
        return nil, errors.New("no_tags")
    } else if len(tags) > settings.Limit.TagsPerThread {
        return nil, errors.New("too_many_tags")
    }

    t := h.newThread()
    t.Tags = append(tags, "!!_all")
    h.Threads[t.Id] = t
    h.attachTags(t)
    h.pruneThreads()

    return t, nil
}

func (h *hive) newThread() *thread {
    tid := threadId( strconv.FormatInt( int64(h.ThreadCount), 32) )
    h.ThreadCount++

    return &thread{
        Id:         tid,
        RandomMark: uint(rand.Uint32()),
        Posts:      []*post{},
        PostById:   map[postLid]*post{},
        PostsByAddr: map[string][]*post{},
        HanGen:     hanGenerator(),
        HanMap:     map[string]han{},
        UserIds:    map[uint64]bool{},
        Listeners:  []wsRegistration{},
    }
}

func (h *hive) pruneThreads() {
    if h.ThreadCount > uint(settings.Limit.Threads) {
        all, _ := h.tags["!!_all"]
        oldest := all.Normal.Threads.Back().Value.(*thread).Id
        h.DeleteThread(oldest)
    }
}

func (h *hive) attachTags(t *thread) {
    for _, tag := range t.Tags {
        name := tag
        if name == "!!_nsfw" {
            t.Nsfw = true
        }

        subject, ok := h.tags[name]
        if !ok {
            subject = newTag(name)
            h.tags[name] = subject
        }
        subject.AddThread(t)
    }

    h.tags.genAutocompleteXml()
}

func cleanUserTags(p *post) ([]string, error) {
    alreadyAdded := map[string]bool{}
    out := []string{}

    for _, label := range p.Tags {
        label = strings.ToLower(label)

        if !validTag(label) && !p.Role.PostSystemThreads {
            return []string{}, errors.New("prohibited_tags")
        }

        if len(label) > settings.Limit.TagLength {
            return []string{}, errors.New("tag_too_long")
        }

        if _, ok := alreadyAdded[label]; ok {
            continue
        }

        out = append(out, label)
        alreadyAdded[label] = true
    }

    return out, nil
}



func (h *hive) AddThreadListener(reg wsRegistration) {
    t, ok := h.Threads[reg.Thread]
    if !ok {
        return
    }

    h.Threads[reg.Thread].Listeners = append(t.Listeners, reg)
}

func lockThread(tid threadId, val bool) {
    hiveReq(func(h *hive) {
        t, ok := h.Threads[tid]
        if !ok {
            return
        }

        t.Locked = val
        t.UpdateThreadSummary()
        pageCache.SetStale(string(tid), t.Hidden)
    })

    cmd := ("UPDATE threads SET locked = ?1 WHERE id = ?2;")
    if _, e := db.Exec(cmd, val, string(tid)); e != nil {
        log.Panic(e)
    }
}

func (h *hive) StickyThread(tid threadId, stickyTags []string) {
    t, ok := h.Threads[tid]
    if !ok {
        return
    }

    mustSticky := func(name string) bool {
        for _, tag := range stickyTags {
            if tag == name {
                return true
            }
        }
        return false
    }

    for _, tagRef := range t.Tags {
        if mustSticky(tagRef) {
            if tag, ok := h.tags[tagRef]; ok {
                tag.StickyThread(t)
                t.setSticky(tagRef)
            }
        }
    }

    for _, tagRef := range t.StickyTags {
        if !mustSticky(tagRef) {
            if tag, ok := h.tags[tagRef]; ok {
                tag.UnstickyThread(t)
                t.setUnsticky(tagRef)
            }
        }
    }

    dbUpdateTags(t)
}

func (t *thread) setSticky(name string) {
    for i, tag := range t.Tags {
        if tag == name {
            t.Tags = append(t.Tags[:i], t.Tags[i+1:]...)
            t.StickyTags = append(t.StickyTags, tag)
        }
    }
}

func (t *thread) setUnsticky(name string) {
    for i, tag := range t.StickyTags {
        if tag == name {
            t.StickyTags = append(t.StickyTags[:i], t.StickyTags[i+1:]...)
            t.Tags = append(t.Tags, tag)
        }
    }
}

func dbUpdateTags(t *thread) {
    tags := strings.Join(t.Tags, " ")
    tagsCmd := ("UPDATE threads SET tags = ?1 WHERE id = ?2;")
    if _, e := db.Exec(tagsCmd, tags, string(t.Id)); e != nil {
        log.Panic(e)
    }

    sticky := strings.Join(t.StickyTags, " ")
    stickyCmd := ("UPDATE threads SET sticky_tags = ?1 WHERE id = ?2;")
    if _, e := db.Exec(stickyCmd, sticky, string(t.Id)); e != nil {
        log.Panic(e)
    }
}

// create copy of posts in received pointers, set copies' showaddr to true,
// then run template.
func (h *hive) ModPosts(posts []*post) []byte {
    postCopies := []post{}
    for _, p := range posts {
        postCopy := *p
        postCopy.AdminInfo = true
        postCopies = append(postCopies, postCopy)
    }

    data := struct {
        Posts       []post
        BanReasons  map[string]banReason
    }{ postCopies, settings.BanReasons }

    buf := new(bytes.Buffer)
    e := templates.ExecuteTemplate(buf, "mod_posts", data)
    if e != nil {
        log.Println(e)
    }

    return buf.Bytes()
}

// Lookup posts by ref, set to hidden, and re-template affected threads.
func (h *hive) HidePost(p *post) {
    affectedThreads := map[threadId]*thread{}
    p.Hidden = true

    if t, ok := h.Threads[p.ParentThread]; ok {
        affectedThreads[p.ParentThread] = t
    }

    for queue, _ := range p.ReportHistory {
        if t, ok := h.Threads[queue]; ok {
            affectedThreads[queue] = t
        }
    }

    for _, t := range affectedThreads {
        pageCache.SetStale(string(t.Id), t.Hidden)
    }
}

// Remove thread from threadLists in each tag, then from the hive itself,
// then delete from DB.
func (h *hive) DeleteThread(tid threadId) {
    t, ok := h.Threads[tid]
    if !ok {
        log.Printf("Could not find thread %s to delete", string(tid))
        return
    }

    for _, label := range t.Tags {
        if tag, ok := h.tags[label]; ok {
            tag.Normal.RemoveThread(t)
        }
    }

    for _, label := range t.StickyTags {
        if tag, ok := h.tags[label]; ok {
            tag.Sticky.RemoveThread(t)
        }
    }

    log.Println("Deleting thread " + string(tid))
    delete(h.Threads, tid)
    pageCache.Purge(string(tid))

    cmd := ("DELETE FROM threads WHERE id = ?1;")
    if _, e := db.Exec(cmd, string(tid)); e != nil {
        log.Panic(e)
    }
}

func (h *hive) GetPost(gid postGid) *post {
    if p, ok := h.Posts[gid]; ok {
        return p
    } else {
        return nil
    }
}

func (h *hive) PostsByAddr(ip string) []*post {
    out := []*post{}

    for _, t := range h.Threads {
        threadPosts, ok := t.PostsByAddr[ip]
        if !ok {
            continue
        }

        out = append(out, threadPosts...)
    }

    return out
}

// create function for escaping comments and inserting markup.
func genMarkup() func(s string) string {
    UnescapeBBCode := strings.NewReplacer(
		"[spoiler]",        `<span class="spoiler">`,
        "[/spoiler]",       `</span>`,
		"[code]",           `<code>`,
        "[/code]",          `</code>`,
    )

    return func(text string) string {
        text = UnescapeBBCode.Replace(template.HTMLEscapeString(text))
        text = strings.TrimSpace(text)

        lines := []string{}
        var newline int

		for _, line := range strings.Split(text, "\n") {
            line = strings.Trim(line, "\n")
            if strings.TrimSpace(line) == "" {
                newline++
                if newline < 2 {
                    lines = append(lines, line)
                }
                continue
            }

            newline = 0
            if strings.HasPrefix(line, "&gt;") {
                line = `<span class="line_quote">` + line + `</span>`
            }
            lines = append(lines, line)
		}
        return strings.TrimSpace( strings.Join(lines, "") )
	}
}

func validTag(s string) bool {
    return !strings.HasPrefix(s, "!?_")
}

