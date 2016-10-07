package main

import (
	"golang.org/x/crypto/openpgp"
	"golang.org/x/net/websocket"
	"database/sql"
	"github.com/gorilla/context"
	"github.com/gorilla/sessions"
	"github.com/jessevdk/go-flags"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	txt "text/template"
	"time"
)

const configDir = "config"

var staffSessions = sessions.NewCookieStore()

var db *sql.DB
var mediaStore *library
var templates *template.Template
var text *txt.Template
var settings *tolxankaConfigToml
var keyRing *openpgp.EntityList

type Options struct {
	Insecure bool `long:"insecure-mode" description:"Insecure Mode" optional:"yes" optional-value:"false"`
}

func setConfiguration() {
	settings = readConfigToml(configDir)
	opts := Options{}
	flags.Parse(&opts)
	settings.Debug.insecureMode = opts.Insecure
}

// Dump hive and quit if appropriate upon receving certain signals.
func watchSignals() chan os.Signal {
	watcher := make(chan os.Signal)

	go func() {
		for {
			sig := <-watcher
			switch sig {
			case syscall.SIGABRT:
				fallthrough
			case syscall.SIGINT:
				fallthrough
			case syscall.SIGTERM:
				os.Exit(0)
			case syscall.SIGUSR1:
			}
		}
	}()

	return watcher
}

func installHandlers() {
	// Handlers accessing threshold restricted resources
	http.Handle("/static/", http.FileServer(http.Dir("./data/")))
	handleThreshold("/", showTagQuery(0, "!!_all", "catalog"))
	handleThreshold("/cat/", showCatQueryFromURI)
	handleThreshold("/sum/", showSumQueryFromURI)
	handleThreshold("/i/", showFullMedia)
	handleThreshold("/t/", showThread)
	handleThreshold("/cat_search", showCatQuery)
	handleThreshold("/sum_search", showSumQuery)
	handleThreshold("/report_post_landing/", showReportLanding)
	handleThreshold("/tags_autocomplete", getAutocomplete)

	http.Handle("/ws_post/", websocket.Handler(threadUpdater))
	http.HandleFunc("/robots.txt", showRobots)
	http.HandleFunc("/th/", showThumbImage)
	http.HandleFunc("/report_post", postReport)
	http.HandleFunc("/post", postComment)
	http.HandleFunc("/new_thread", postThread)
	http.HandleFunc("/admin_login", showAdminLogin)
	http.HandleFunc("/admin_delete_thread/", showDeleteThread)
	http.HandleFunc("/admin_post_delete_thread", postDeleteThread)
	http.HandleFunc("/admin_response", postAdminResponse)
	http.HandleFunc("/admin_mod_posts", postModPosts)
	http.HandleFunc("/admin_mod_posts_landing", postModPostsLanding)
	http.HandleFunc("/admin_lock_thread/", postLock)
	http.HandleFunc("/admin_sticky_thread_landing/", showStickyLanding)
	http.HandleFunc("/admin_sticky_thread", postSticky)
	http.HandleFunc("/admin_rights", showAdminRights)
	http.HandleFunc("/posts_by_user/", postPostsByUser)
}

func parseTemplates() {
	text = txt.New("report")
	_, e := text.ParseGlob("./templates/*.txt")
	if e != nil {
		panic(e)
	}

	templates = template.New("")
	templates.Funcs(helperMap())

	_, e = templates.ParseGlob("./templates/*.xhtml")
	if e != nil {
		log.Panic(e)
	}
}

// Parse templates, initialized image store, thread cache and tag map,
// start post sequencer and web socket broadcaster, read posts back in from
// dump file, install http handlers and listen.
func main() {
	log.Println("Starting Tolxanka.")
	rand.Seed(time.Now().UTC().UnixNano())

	setConfiguration()
	log.Printf("Using configuration:\n%s\n", settings.String())

	parseTemplates()
	staffSessions = initSessionStore()
	db = initializeDatabase()
	initDbLoop()
	mediaStore = newLibrary()
	pageCache = newByteCache()
	siteUsers = newUserMap()
	signal.Notify(watchSignals())
	initSequencer()
	startFfmpegWorkers()
	installHandlers()

	log.Printf("Listening on port %d", settings.General.ListenPort)

	port := ":" + strconv.Itoa(settings.General.ListenPort)
	err := http.ListenAndServe(port, context.ClearHandler(http.DefaultServeMux))
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
