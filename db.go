
package main

import (
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
    "log"
    "strings"
    "time"
)

var persistBan chan *userBan
var persistPost chan *post
var persistThread chan *thread
var persistMedia chan *media
var sqlInsertThread, sqlInsertPost, sqlInsertMedia, sqlInsertBan *sql.Stmt

func prepareStatements(db *sql.DB) {
    prepare := func(cmd string) *sql.Stmt {
        if stmt, e := db.Prepare(cmd); e != nil {
            panic(e)
        } else {
            return stmt
        }
    }

    sqlInsertThread = prepare(
            "INSERT INTO threads " +
            "(id, random_mark, updated, tags, sticky_tags, locked, hidden) " +
            "VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7);")

    sqlInsertPost = prepare(
            "INSERT INTO posts " +
            "(comment, user_addr, media, media_name, global_id, local_id, reply_to, " +
            "time, parent_thread, hidden, authority) " +
            "VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11);")

    sqlInsertMedia = prepare(
            "INSERT INTO media " +
            "(hash, thumb, type, info, size, ban_reason) " +
            "VALUES (?1, ?2, ?3, ?4, ?5, ?6);")

    sqlInsertBan = prepare(
            "INSERT INTO bans " +
            "(user_addr, reason, description, start_time, end_time) " +
            "VALUES (?1, ?2, ?3, ?4, ?5);")
}

func createSchema(db *sql.DB) {
    run := func(cmd string) {
        if _, e := db.Exec(cmd); e != nil {
            panic(e)
        }
    }

    run(`CREATE TABLE IF NOT EXISTS threads(
                id              TEXT PRIMARY KEY,
                random_mark     INTEGER NOT NULL,
                updated         INTEGER NOT NULL,
                tags            TEXT NOT NULL,
                sticky_tags     TEXT NOT NULL,
                locked          INTEGER NOT NULL,
                hidden          INTEGER NOT NULL);`)

    run(`CREATE TABLE IF NOT EXISTS posts(
                id              INTEGER PRIMARY KEY,
                comment         TEXT NOT NULL,
                user_addr       TEXT NOT NULL,
                media           TEXT REFERENCES media(hash),
                media_name      TEXT NOT NULL,
                global_id       INTEGER NOT NULL,
                local_id        INTEGER NOT NULL,
                reply_to        INTEGER NOT NULL,
                time            INTEGER NOT NULL,
                parent_thread   TEXT REFERENCES threads(id) ON DELETE CASCADE,
                hidden          INTEGER NOT NULL,
                authority       TEXT NOT NULL);`)

    run(`CREATE TABLE IF NOT EXISTS media(
                hash            TEXT PRIMARY KEY,
                thumb           TEXT NOT NULL,
                type            TEXT NOT NULL,
                info            TEXT NOT NULL,
                size            INTEGER NOT NULL,
                ban_reason      TEXT NOT NULL);`)

    run(`CREATE TABLE IF NOT EXISTS bans(
                id              INTEGER PRIMARY KEY,
                user_addr       TEXT NOT NULL,
                reason          TEXT NOT NULL,
                description     TEXT NOT NULL,
                start_time      INTEGER NOT NULL,
                end_time        INTEGER NOT NULL);`)
}

func initializeDatabase() *sql.DB {
    var e error
    db, e := sql.Open("sqlite3", settings.Database.Name)
    if e != nil {
        panic(e)
    }

    if _, e := db.Exec("PRAGMA foreign_keys = ON;"); e != nil {
        log.Panic(e)
    }

    createSchema(db)
    prepareStatements(db)
    return db
}

func dbInsertThread(tx *sql.Tx, t *thread) (sql.Result, error) {
    return tx.Stmt(sqlInsertThread).Exec(
        string(t.Id), t.RandomMark, t.Updated.Unix(),
        strings.Join(t.Tags, " "), strings.Join(t.StickyTags, " "),
        t.Locked, t.Hidden)
}

func dbInsertPost(tx *sql.Tx, p *post) (sql.Result, error) {
    var imgHash *string
    var role string

    if p.Media != nil {
        imgHash = &p.Media.Hash
    }

    if p.ShowRole {
        role = p.RoleName
    }

    return tx.Stmt(sqlInsertPost).Exec(
        p.Comment, p.UserAddr, imgHash, p.MediaName, p.GlobalId, p.LocalId,
        p.ReplyTo, p.Time.Unix(), string(p.ParentThread), p.Hidden, role)
}

func dbInsertMedia(tx *sql.Tx, i *media) (sql.Result, error) {
    var banReason string
    if i.Blocked != nil {
        banReason = i.Blocked.Name
    }

    return tx.Stmt(sqlInsertMedia).Exec(i.Hash, i.Thumb, i.MediaType,
                                        i.InfoString, i.Size, banReason)
}

func dbInsertBan(tx *sql.Tx, b *userBan) (sql.Result, error) {
    return tx.Stmt(sqlInsertBan).Exec(b.Addr, b.Reason.Name,
                    b.Reason.Description, b.Start.Unix(), b.End.Unix())
}

func dumpBans(bans chan *userBan) {
    wrapTransaction(db, func(tx *sql.Tx) {
        for ; len(bans) != 0; {
            if _, e := dbInsertBan(tx, <-bans); e != nil {
                log.Println(e)
            }
        }
    })
}

func dumpPosts(posts chan *post) {
    wrapTransaction(db, func(tx *sql.Tx) {
        for ; len(posts) != 0; {
            if _, e := dbInsertPost(tx, <-posts); e != nil {
                log.Println(e)
            }
        }
    })
}

func dumpThreads(threads chan *thread) {
    wrapTransaction(db, func(tx *sql.Tx) {
        for ; len(threads) != 0; {
            if _, e := dbInsertThread(tx, <-threads); e != nil {
                log.Println(e)
            }
        }
    })
}

func dumpMedia(ms chan *media) {
    wrapTransaction(db, func(tx *sql.Tx) {
        for ; len(ms) != 0; {
            if _, e := dbInsertMedia(tx, <-ms); e != nil {
                log.Println(e)
            }
        }
    })
}

func wrapTransaction(db *sql.DB, f func(*sql.Tx)) {
    tx, e := db.Begin()
    if e != nil {
        log.Println(e)
    }

    f(tx)

    if e := tx.Commit(); e != nil {
        log.Println(e)
    }
}

func (h *hive) recoverFromDatabase() {
    log.Println("Recovering from database...")
    h.recoverThreads()
    h.recoverPosts()
}

func (h *hive) recoverThreads() {
    query := "SELECT id, random_mark, updated, tags, " +
                "sticky_tags, locked, hidden FROM threads;"

    rows, e := db.Query(query)
    if e != nil {
        log.Panic(e)
    }
    defer rows.Close()

    for rows.Next() {
        t := h.newThread()
        var updated int64
        var tid, tags, stickyTags string
        e := rows.Scan(&tid, &t.RandomMark, &updated,
                            &tags, &stickyTags, &t.Locked, &t.Hidden)

        if e != nil {
            log.Panic(e)
        }

        t.Id = threadId(tid)
        t.Updated = time.Unix(updated, 0)
        t.Tags = strings.Fields(tags)
        t.StickyTags = strings.Fields(stickyTags)
        h.Threads[t.Id] = t
        h.attachTags(t)
    }

    for _, t := range h.Threads {
        t.recoverThreadStickiness()
    }
}

func (t *thread) recoverThreadStickiness() {
    hiveReq(func(h *hive) {
        for _, tagRef := range t.StickyTags {
            if tag, ok := h.tags[tagRef]; ok {
                tag.StickyThread(t)
                t.setSticky(tagRef)
            }
        }
    })
    dbUpdateTags(t)
}

func (h *hive) recoverPosts() {
    query := "SELECT comment, user_addr, media, media_name, global_id, " +
        "local_id, reply_to, time, parent_thread, hidden, authority FROM posts;"

    rows, e := db.Query(query)
    if e != nil {
        log.Panic(e)
    }
    defer rows.Close()

    for rows.Next() {
        p := &post{}
        var tid string
        var imgHash *string
        var postTime *uint64
        e := rows.Scan( &p.Comment, &p.UserAddr, &imgHash, &p.MediaName,
                        &p.GlobalId, &p.LocalId, &p.ReplyTo, &postTime,
                        &tid, &p.Hidden, &p.RoleName)

        if e != nil {
            log.Panic(e)
        }

        var ok bool
        if imgHash != nil {
            if p.Media, ok = mediaStore.byHash[*imgHash]; !ok {
                log.Panic("post media hash not found during recovery")
            }
        }

        if p.RoleName != "" {
            p.Role = settings.Roles[p.RoleName]
            p.ShowRole = true
        }

        p.Recovered = true
        p.Bytes = []byte{}
        p.Replies = []postRef{}
        p.ParentThread = threadId(tid)
        p.Time = time.Unix(int64(*postTime), 0)
        h.AddPost(p)
    }

    for _, t := range h.Threads {
        t.UpdateThreadSummary()
    }
}

func initDbLoop() {
    if settings.Database.PostQueueSize < 1 ||
            settings.Database.ThreadQueueSize < 1 {
        log.Fatal("Database queue sizes must be greater than zero")
    }

    persistPost = make(chan *post, settings.Database.PostQueueSize)
    persistThread = make(chan *thread, settings.Database.ThreadQueueSize)
    persistMedia = make(chan *media, settings.Database.MediaQueueSize)
    persistBan = make(chan *userBan, settings.Database.BanQueueSize)
    dumpTicker := time.NewTicker(settings.Database.DumpInterval.Duration)

    go func() {
        for {
            <-dumpTicker.C
            dumpThreads(persistThread)
            dumpMedia(persistMedia)
            dumpPosts(persistPost)
            dumpBans(persistBan)
        }
    }()
}

