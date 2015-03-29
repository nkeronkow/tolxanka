//
//  users.go
//
//  Structures for imposing general constraints on user behavior.
//  Temporarily logs user actions and identifies when they've exceeded
//  certain limits. The limits themselves are set in the config/settings.toml
//  file. 
//              

package main

import (
    "log"
    "net/http"
    "strings"
    "sync"
    "time"
)

var siteUsers userMap

// map IP addresses to users.
type userMap struct {
    users   map[string]*boardUser
    mtx     sync.RWMutex
}

func newUserMap() userMap {
    um := userMap{}
    um.users = map[string]*boardUser{}
    um.readBans()

    return um
}

// Fetch user from map, or create user. Then record occurence.
func (um *userMap) InThreshold(addr, param string) bool {
    um.mtx.Lock()
    defer um.mtx.Unlock()

    return um.getUser(addr).Thresholds[param].Record()
}

func (um *userMap) IsUserBanned(addr string) *userBan {
    um.mtx.Lock()
    defer um.mtx.Unlock()

    ban := um.getUser(addr).Ban

    if ban != nil && time.Now().Before(ban.End) {
        return ban
    } else {
        return nil
    }
}

func createBan(addr string, reason banReason) *userBan {
    ban := &userBan{}
    ban.Addr =      addr
    ban.Reason =    reason
    ban.Start =     time.Now()
    ban.Duration =  time.Duration(reason.Length) * time.Hour * 24
    ban.End =       ban.Start.Add(ban.Duration)
    return ban
}

func (um *userMap) IssueBanByName(addr string, name string) *userBan {
    reason, ok := settings.BanReasons[name]
    if !ok {
        log.Panic("IssueBanByName: reason not found")
    }

    return um.IssueBan(addr, reason)
}

func (um *userMap) IssueBan(addr string, reason banReason) *userBan {
    log.Println("issuing ban")
    ban := createBan(addr, reason)
    persistBan <-ban
    um.mtx.Lock()
    defer um.mtx.Unlock()
    um.getUser(addr).Ban = ban
    return ban
}

func (um *userMap) getUser(addr string) *boardUser {
    user, ok := um.users[addr]
    if !ok {
        user = newBoardUser(addr)
        um.users[addr] = user
    }

    return user
}

// Individual user struct.
type boardUser struct {
    Thresholds  map[string]*threshold
    Challenge   *adminChallenge
    Ban         *userBan
}

func newBoardUser(addr string) *boardUser {
    user := new(boardUser)
    user.Thresholds = map[string]*threshold{}

    for name, ts := range settings.Thresholds {
        user.Thresholds[name] =
            newThreshold(ts.Times, ts.Duration.Duration)
    }

    return user
}

// thresholds track the number of occurences of a certain action within a
// specified time period.
type threshold struct {
    Occurences  []time.Time
    Interval    time.Duration
    Max         int
}

func newThreshold(max int, interval time.Duration) *threshold {
    return &threshold{ make([]time.Time, max), interval, max }
}

// Record this occurence. If outside the acceptable parameters, return false,
// otherwise, true.
func (th threshold) Record() bool {
    now := time.Now()

    if len(th.Occurences) < th.Max {
        th.Occurences = append(th.Occurences, now)
        return true
    }

    for i, o := range th.Occurences {
        sinceLast := now.Sub(o)

        if sinceLast > th.Interval {
            th.Occurences[i] = now
            return true
        }
    }

    return false
}

// wrap http handlers with ban check and optional threshold checking.
func handleThreshold(path string, fn http.HandlerFunc) {
    page := func(w http.ResponseWriter, r *http.Request) {
        if siteUsers.IsPageViewAllowed(w, r) {
            fn(w, r)
        }
    }

	http.HandleFunc(path, page)
}

// Special check for page views. Check that the user isn't banned, and also
// optionally check that he isn't beyond the pageRequest threshold.
func (um *userMap) IsPageViewAllowed(
        w http.ResponseWriter, r *http.Request) bool {

    addr := strings.SplitN(r.RemoteAddr, ":", 2)[0]
    ban := um.IsUserBanned(addr)

    if !um.InThreshold(addr, "PageRequest") {
        if ban != nil && ban.Reason.Name == "Dos" {
            return false
        }

        um.IssueBan(addr, banReason{
            Name:   "Dos",
            Description: "Abnormal rate of HTTP traffic",
            Length: 1,
        })
    }

    if ban := um.IsUserBanned(addr); ban != nil {
        e := templates.ExecuteTemplate(w, "banned", struct{
            Ban         *userBan
            Settings    *tolxankaConfigToml
        }{ban, settings})

        if e != nil {
            log.Panic(e)
        }

        return false
    }

    return true
}


type userBan struct {
    Addr        string
    Reason      banReason
    Start       time.Time
    End         time.Time
    Duration    time.Duration
}

func (um *userMap) readBans() {
    um.mtx.Lock()
    defer um.mtx.Unlock()

    query := ("SELECT user_addr, reason, description, start_time, end_time " +
              "FROM bans;")

    rows, e := db.Query(query)
    if e != nil {
        log.Panic(e)
    }
    defer rows.Close()

    for rows.Next() {
        ban := &userBan{}
        var start, end int64
        e := rows.Scan(&ban.Addr, &ban.Reason.Name, &ban.Reason.Description,
                                                                &start, &end)

        if e != nil {
            log.Panic(e)
        }

        ban.Start = time.Unix(start, 0)
        ban.End = time.Unix(end, 0)
        ban.Duration = ban.End.Sub(ban.Start)
        um.getUser(ban.Addr).Ban = ban
        log.Println(ban)
    }
}

// Issue a timed per-ip challenge string at the admin login page.
func (um *userMap) IssueAdminChallenge(addr string) (challenge string, isNew bool) {
    um.mtx.Lock()
    defer um.mtx.Unlock()

    user := um.getUser(addr)
    now := time.Now()
    expires := now.Add(settings.Admin.ChallengeDuration.Duration)

    if user.Challenge == nil || now.After(user.Challenge.Expiration) {
        isNew = true
        user.Challenge = &adminChallenge{
            Text: string( randomBytes(settings.Admin.ChallengeLength) ),
            Expiration: expires,
        }
    }

    log.Printf("%s - %v", addr, user.Challenge.Expiration)
    return user.Challenge.Text, isNew
}
