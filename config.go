package main

import (
	"bytes"
	"fmt"
	"github.com/BurntSushi/toml"
	"io/ioutil"
	"log"
	"path/filepath"
	"reflect"
	"regexp"
	"time"
	"unicode"
)

type tolxankaConfigToml struct {
	Debug    debugConf
	General  generalConf
	Catalog  catalogConf
	Limit    limitConf
	Admin    adminConf
	Media    mediaConf
	Image    imageConf
	Video    videoConf
	Audio    audioConf
	SpamTrap spamTrapConf
	Notify   notifyConf
	Database dbConf

	Staff       map[string]Staff
	Roles       map[string]Role
	Thresholds  map[string]thresholdSetting
	BanReasons  map[string]banReason
	WordFilters []*wordFilter
}

type debugConf struct {
	insecureMode bool
}

type generalConf struct {
	SiteName              string
	ListenPort            int
	PassthroughDelay      duration
	AutoDeleteThreshold   int
	PostTimeFormat        string
	SummaryPostTailLength int

	maxFileSize int64
}

type catalogConf struct {
	SummaryCharLimit int
	PageRange        int
	ThreadsPerPage   int
}

type limitConf struct {
	Threads         int
	PostsPerThread  int
	TagsPerThread   int
	CommentLength   int
	TagLength       int
	NewlinesPerPost int
}

type adminConf struct {
	ChallengeLength   int
	ChallengeDuration duration
	CookieName        string
	CookieLifetime    duration
}

type mediaConf struct {
	Path          string
	ValidReferers []string
	CacheSize     int
}

type imageConf struct {
	AcceptedFileFormats []string
	ThumbWidth          int
	ThumbHeight         int
	MaxSize             int64
}

type videoConf struct {
	FfmpegPath          string
	FfprobePath         string
	Workers             int
	ThumbnailSeekTime   duration
	AcceptedFileFormats []string
	MaxSize             int64
}

type audioConf struct {
	AcceptedCodecs      []string
	AcceptedFileFormats []string
	ThumbnailFile       string
	MaxSize             int64
}

type spamTrapConf struct {
	DuplicateFields    int
	FieldDisplay       []int
	FieldHide          []int
	FieldPrefix        string
	ThreadFormLifetime duration
}

type notifyConf struct {
	Addr       string
	Password   string
	SMTPServer string
	FromEmail  string
}

type dbConf struct {
	Name            string
	DumpInterval    duration
	BanQueueSize    int
	PostQueueSize   int
	ThreadQueueSize int
	MediaQueueSize  int
}

type Role struct {
	Title                string
	Marker               string
	Color                string
	PostWithRole         bool
	ViewRestrictedTags   bool
	SeeHiddenThreads     bool
	PostInLockedThread   bool
	PostSystemThreads    bool
	LockThread           bool
	StickyThread         bool
	DeleteThread         bool
	DeletePost           bool
	BanUser              bool
	BlockImage           bool
	ShowUserPosts        bool
	RecommendBan         bool
	ReceiveNotifications bool
}

type Staff struct {
	Role      string
	Active    bool
	Email     string
	PublicKey string
}

type thresholdSetting struct {
	Times    int
	Duration duration
}

type banReason struct {
	Name        string
	Description string
	Length      uint64
}

type wordFilter struct {
	Pattern string
	Ban     string
	Regexp  *regexp.Regexp
}

type duration struct {
	time.Duration
}

func (dur *duration) UnmarshalText(text []byte) error {
	var e error
	dur.Duration, e = time.ParseDuration(string(text))
	return e
}

func concatenateToml(dirPath string) []byte {
	fileStats, e := ioutil.ReadDir(dirPath)
	if e != nil {
		log.Panic(e)
	}

	var all bytes.Buffer

	for _, stats := range fileStats {
		path := filepath.Join(dirPath, stats.Name())
		fmt.Println(path)
		if filepath.Ext(path) != ".toml" {
			continue
		}

		fmt.Println("reading " + path)

		raw, e := ioutil.ReadFile(path)
		if e != nil {
			log.Fatal("Error reading config file: " + e.Error())
		}

		all.Write(raw)
	}

	return all.Bytes()
}

func readConfigToml(path string) *tolxankaConfigToml {
	raw := concatenateToml(path)
	cfg := &tolxankaConfigToml{}

	if e := toml.Unmarshal(raw, cfg); e != nil {
		panic(e)
	}

	for _, filter := range cfg.WordFilters {
		filter.Regexp = regexp.MustCompile(filter.Pattern)
	}

	for k, v := range cfg.BanReasons {
		v.Name = k
		cfg.BanReasons[k] = v
	}

	var e error
	audioThumbnail, e = ioutil.ReadFile(cfg.Audio.ThumbnailFile)
	if e != nil {
		log.Panic(e)
	}

	cfg.Media.CacheSize *= (1000 * 1000)
	cfg.Image.MaxSize *= (1000 * 1000)
	cfg.Video.MaxSize *= (1000 * 1000)
	cfg.Audio.MaxSize *= (1000 * 1000)
	cfg.General.maxFileSize =
		max(cfg.Image.MaxSize, cfg.Video.MaxSize, cfg.Audio.MaxSize)

	return cfg
}

func max(nums ...int64) int64 {
	maximum := nums[0]
	for _, n := range nums {
		if n > maximum {
			maximum = n
		}
	}
	return maximum
}

func (cfg *tolxankaConfigToml) String() string {
	return printFields(cfg)
}

func printFields(x interface{}) string {
	out := ""
	v := reflect.ValueOf(x)

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		name := v.Type().Field(i).Name

		if unicode.IsLower(rune(name[0])) {
			continue
		}

		if f.Kind() == reflect.Struct {
			out += "\n" + printFields(f.Interface())
			continue
		}

		out += fmt.Sprintf("%25s %15s <%v>\n", name, f.Type(), f.Interface())
	}

	return out
}
