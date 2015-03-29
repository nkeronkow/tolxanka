package main

import (
    "net/http"
    "sync"
)

var pageCache *byteCache

// cached limited-lifetime raw page data for page 0 indifidual tag queries.
type byteCache struct {
    pages   map[string]*bPage
    mtx     sync.RWMutex
}

type bPage struct {
    Content     []byte
    Hidden      bool
    Fresh       bool
}

func newByteCache() *byteCache {
    c := new(byteCache)
    c.pages = map[string]*bPage{}
    return c
}

func (c *byteCache) SetStale(title string, hide bool) {
    c.mtx.Lock()
    defer c.mtx.Unlock()

    c.pages[title] = &bPage{ []byte{}, hide, false }
}

func (c *byteCache) Purge(title string) {
    c.mtx.Lock()
    defer c.mtx.Unlock()

    delete(c.pages, title)
}


// send page from cache. If necessary, validate admin credentials first. If
// cache is stale, temporarily release mutex and regenerate page from the
// hive.
func (c *byteCache) Get(title string,
            w http.ResponseWriter, r *http.Request) bool {

    c.mtx.RLock()
    defer c.mtx.RUnlock()

    bp, ok := c.pages[title]
    if !ok {
        return false
    }

    if bp.Hidden && !getStaffRole(r).SeeHiddenThreads {
        return false
    }

    if !bp.Fresh {
        c.mtx.RUnlock()
        pageBytes := sendPageRequest(title)
        c.mtx.RLock()

        bp.Content = pageBytes
        bp.Fresh = true
    }

    w.Header().Set("Content-Encoding", "gzip")
    w.Header().Set("Content-Type", "application/xhtml+xml; charset=UTF-8")
    w.Write(bp.Content)
    return true
}

func sendPageRequest(title string) []byte {
    reply := make(chan []byte, 1)
    threadRequest <-threadReq{ threadId(title), reply }
    return <-reply
}

