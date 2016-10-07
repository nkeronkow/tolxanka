package main

import (
	"bytes"
	"golang.org/x/net/websocket"
	"compress/gzip"
	"html/template"
	"io"
	"log"
	"math/rand"
	"time"
)

type postGid uint
type postLid uint
type threadId string

type postRef struct {
	Global postGid
	Local  postLid
	Thread threadId
}

type hslColor struct {
	Hue        int
	Saturation int
	Lightness  int
}

type han struct {
	Char  string
	Color hslColor
	Seq   uint
	Ident uint64
}

type AuthLevel uint

const (
	UserAuth AuthLevel = iota
	AdminAuth
	BotAuth
)

type report struct {
	SubmitterAddr string
	Time          time.Time
}

// Comment and User must be provided when sent to thread server
type post struct {
	Comment   string // Original POST text before processing.
	UserAddr  string // User IP
	MediaName string // Name of attached image.
	Media     *media // Attached image.

	EscapedImageName template.HTML         // Safely escaped image name.
	EscapedComment   template.HTML         // Safely escaped user comment text.
	GlobalId         postGid               // Global post ID
	LocalId          postLid               // Per thread post ID
	ReplyTo          postLid               // Target post ID.
	Time             time.Time             // Time post was added to thread.
	TimeString       string                // User-visible time in postTimeFormat
	ParentThread     threadId              // Parent thread ID.
	Han              han                   // Han char identifying user in thread.
	Replies          []postRef             // List of posts replying to this one.
	Tags             []string              // Relevant tag names if this is first post.
	DesiredUserId    uint64                // User ID requested by user.
	Bytes            []byte                // Raw bytes of templated post.
	OP               bool                  // Post is the first in thread.
	Recovered        bool                  // Post was recovered from glob.
	Hidden           bool                  // Post is administratively hidden.
	Role             Role                  // Staff role of poster.
	RoleName         string                // Name of staff role.
	ShowRole         bool                  // Publicly indicate user's role.
	ReportHistory    map[threadId][]report // map of reports per report queue
	ReportedBy       map[string]bool       // map of reports per report queue
	AdminInfo        bool                  // Show additional info, for admin pages.
	NoDump           bool                  // Internal post, do not dump to DB.
}

type thread struct {
	Id            threadId           // Global thread ID.
	RandomMark    uint               // Random ID to distinguish after restarts.
	CatBytes      []byte             // Templated catalog view template thread.
	SumBytes      []byte             // Templated summary view template thread.
	PostForm      []byte             // Randomly-generate antispam post form.
	Posts         []*post            // Slice of all posts.
	PostById      map[postLid]*post  // map of postIds to posts.
	PostsByAddr   map[string][]*post // map of IPs to posts.
	PostsBytes    []byte             // HTML of all existing posts.
	Updated       time.Time          // Time of last post to thread.
	UpdatedString string             // User-visible and formatted Updated time.
	HanGen        func() han         // Han-generating closure.
	HanMap        map[string]han     // Map of user IPs to han characters.
	UserIds       map[uint64]bool    // Map of taken user IDs.
	Tags          []string           // Names of associated tags.
	StickyTags    []string           // Names of associated sticky tags.
	Count         contentCount       // Various coutns of media types
	Listeners     []wsRegistration   // Websocket connections to broadcast to.
	Locked        bool               // Thread has been locked.
	Hidden        bool               // Thread is administratively hidden.
	NoDump        bool               // Internal thread; do not dump to DB.
	Nsfw          bool               // Not Safe For Work.
	FieldNames    fieldNames         // Randomized antispam field names.
}

type contentCount struct {
	Posts int
	Media int
	Image int
	Video int
	Audio int
}

func (t *thread) BindReply(p *post) {
	target, ok := t.PostById[p.ReplyTo]
	if !ok {
		p.ReplyTo = 0
		return
	}

	target.Replies = append(target.Replies,
		postRef{0, p.LocalId, p.ParentThread})

	t.PostById[p.ReplyTo].PreTemplate()
	t.PostById[p.ReplyTo] = target
}

func (t *thread) AddPost(p *post) {
	var ok bool
	var userId uint64
	p.Han, ok = t.HanMap[p.UserAddr]
	// if true { // For debugging, always generate new Han char.
	if !ok {
		p.Han = t.HanGen()

		if _, ok := t.UserIds[p.DesiredUserId]; !ok {
			userId = p.DesiredUserId
		} else {
			userId = uint64(rand.Int63())
		}

		p.Han.Ident = userId
		t.UserIds[userId] = true
		t.HanMap[p.UserAddr] = p.Han
	}

	t.Count.Posts++

	p.LocalId = postLid(t.Count.Posts)
	p.ReportHistory = map[threadId][]report{}
	p.ReportedBy = map[string]bool{}

	if m := p.Role.Marker; m != "" {
		p.Han = han{Char: m}
	}

	if !p.Recovered {
		p.Time = time.Now()
	}

	p.TimeString = p.Time.Format(settings.General.PostTimeFormat)

	t.incrementMediaCounts(p)
	t.Updated = p.Time
	t.UpdatedString = p.Time.Format(settings.General.PostTimeFormat)
	t.Posts = append(t.Posts, p)
	t.PostById[p.LocalId] = p
	t.PostsByAddr[p.UserAddr] = append(t.PostsByAddr[p.UserAddr], p)
	t.BindReply(p)
	p.PreTemplate()

	t.UpdateThreadSummary()
	pageCache.SetStale(string(t.Id), t.Hidden)

	if !p.Recovered {
		t.broadcastPost(p.Bytes)
	}
}

func (t *thread) incrementMediaCounts(p *post) {
	if p.Media == nil {
		return
	}

	switch p.Media.MediaType {
	case "image":
		t.Count.Image++
	case "audio":
		t.Count.Audio++
	case "video":
		t.Count.Video++
	}
	t.Count.Media++
	mediaStore.IncRef(p.Media)
}

func (t *thread) AddPostToReportQueue(p *post) {
	t.Posts = append(t.Posts, p)
	t.PostById[postLid(t.Count.Posts)] = p
	t.Count.Posts++
	t.UpdateThreadSummary()
	pageCache.SetStale(string(t.Id), t.Hidden)
}

func (t *thread) UpdateThreadSummary() {
	t.CatBytes = templateBytes(t, "cat_post")
	t.SumBytes = templateBytes(t, "sum_post")
}

func (p *post) PreTemplate() {
	postBuf := new(bytes.Buffer)
	e := templates.ExecuteTemplate(postBuf, "post", p)
	if e != nil {
		log.Println(e)
	}
	p.Bytes = postBuf.Bytes()
}

func hslGenerator() func() int {
	color := int(rand.Int31n(360))
	interval := 360
	counter := 1

	return func() int {
		out := color
		counter--

		if counter == 0 {
			interval /= 2
			if interval == 0 {
				interval = 360
			}

			counter = 180 / interval
			color = (color + interval*3) % 360
		} else {
			color = (color + interval*2) % 360
		}

		return out
	}
}

func hanGenerator() func() han {
	var user uint
	first := true
	hslGen := hslGenerator()

	// OP always gets the same designation.
	op := han{
		Char: "主",
		Color: hslColor{
			Hue:        0,
			Saturation: 0,
			Lightness:  0,
		},
		Seq: 0,
	}

	return func() han {
		if first {
			first = false
			return op
		}

		user++
		return han{
			Char: string(0x4e3c + rand.Int63n(0x14c3)),
			Color: hslColor{
				Hue:        hslGen(),
				Saturation: 100,
				Lightness:  30,
			},
			Seq: user,
		}
	}
}

type wsRegister struct {
	Sock     *websocket.Conn
	Finished chan bool
}

// template the post, loop over sockets and write to them. If writing
// fails, remove the socket from the listener slice.
func (t *thread) broadcastPost(buf []byte) {
	msg := string(buf)

	end := len(t.Listeners) - 1
	for i := 0; i <= end; i++ {
		ws := t.Listeners[i].Sock
		if ws == nil {
			continue
		}

		ws.SetReadDeadline(time.Now().Add(time.Duration(30 * time.Second)))
		_, err := io.WriteString(ws, msg)
		if err != nil {
			// log.Println("Write failed, closing socket.")
			ws.Close()
			t.Listeners[i].Sock = nil
			t.Listeners[i].Done <- true
			close(t.Listeners[i].Done)
		}
	}
}

func templateBytes(t *thread, view string) []byte {
	sumBuf := new(bytes.Buffer)
	if e := templates.ExecuteTemplate(sumBuf, view, t); e != nil {
		log.Println(e)
	}
	return sumBuf.Bytes()
}

// Run thread templating, update summary byte slice.
func (t *thread) CreateThreadPage() []byte {
	allPosts := new(bytes.Buffer)
	for _, post := range t.Posts {
		if !post.Hidden {
			allPosts.Write(post.Bytes)
		}
	}
	t.PostsBytes = allPosts.Bytes()
	t.PostForm, t.FieldNames = makePostForm(t.Id)

	buf := new(bytes.Buffer)
	gz := gzip.NewWriter(buf)

	type data struct {
		Thread   *thread
		Settings *tolxankaConfigToml
	}

	e := templates.ExecuteTemplate(gz, "thread", data{t, settings})
	if e != nil {
		log.Println(e)
	}

	gz.Close()

	return buf.Bytes()
}

func gzipPage(page *bytes.Buffer) []byte {
	buf := new(bytes.Buffer)
	gz := gzip.NewWriter(buf)

	page.WriteTo(gz)

	gz.Close()
	return buf.Bytes()
}

// sort interface implementation for posts.
type postList []*post

func (ps postList) Len() int      { return len(ps) }
func (ps postList) Swap(i, j int) { ps[i], ps[j] = ps[j], ps[i] }
func (ps postList) Less(i, j int) bool {
	return ps[i].Time.Before(ps[j].Time)
}
