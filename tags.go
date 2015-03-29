/*
    tags.go

    Describes tags and related querying and automation logic. This is
    meant to remain abstracted behind the same mutexed interface as hive.go;
    the structures in this file should not leak out to the general environment.
*/
package main

import (
    "bytes"
    "compress/gzip"
    "container/list"
    "fmt"
    "log"
    "html/template"
    "net/http"
    "sort"
    "strings"
    "sync"
)

type mutexedBytes struct {
    Data    []byte
    mtx     sync.RWMutex
}

func (mb mutexedBytes) WriteList(w http.ResponseWriter) {
    mb.mtx.RLock()
    defer mb.mtx.RUnlock()
    w.Write(mb.Data)
}

var tagSearch mutexedBytes

type tagMap map[string]*tag

type parsedQuery struct {
    Merge       []string
    Filter      []string
    Exclude     []string
    Page        int
    View        string
    QueryString string
    Admin       bool

    WriteOut    http.ResponseWriter
}

// Dump basic info from tagMap to an xml file to be referred to in search box
// autocomplete 
func (pile tagMap) genAutocompleteXml() {
    tagSearch.mtx.Lock()
    defer tagSearch.mtx.Unlock()

    buf := new(bytes.Buffer)

    for _, label := range pile {
        name := label.Name
        if strings.HasPrefix(name, "!?_") || strings.HasPrefix(name, "!!_") {
            continue
        }
        line := fmt.Sprintf("%s\n", name)
        buf.WriteString(line)
    }

    tagSearch.Data = buf.Bytes()
}

// Logically filters all threads to include only those which include all
// of the named tags, and returns the specified range.
func (pile tagMap) Query( offset, perQuery int,
            search parsedQuery) ([]*thread, int) {

    var start, middle, end int

    // If the filter contains missing tags, this part of the query can
    // logically never return any results; therefore, empty the filtered tags
    // list. The only possible results will now come from tag merging.
    filterTags := pile.Lookup(search.Filter)
    if len(filterTags) < len(search.Filter) {
        filterTags = []*tag{}
    }

    nextThread := makeResultGenerator(  filterTags,
                                        pile.Lookup(search.Merge    ),
                                        pile.Lookup(search.Exclude  ))

    for ; start < offset; start++ {
        if nextThread() == nil {
            return []*thread{}, start
        }
    }

    var threads []*thread
    for ; middle < perQuery; middle++ {
        t := nextThread()
        if t == nil {
            return threads, start + middle
        }

        if t.Hidden && !search.Admin {
            continue
        }

        threads = append(threads, t)
    }

    for {
        if nextThread() == nil {
            break
        }
        end++
    }

    return threads, start + middle + end
}

func (pile tagMap) GetStickyThreads(labels []string) []*thread {
    out := []*thread{}

    for _, tag := range pile.Lookup(labels) {
        elem := tag.Sticky.Threads.Front()

        for ; elem != nil; elem = elem.Next() {
            out = append(out, elem.Value.(*thread))
        }
    }

    return out
}

func (pile tagMap) Lookup(labels []string) tagList {
    out := []*tag{}
    for _, name := range labels {
        if v, ok := pile[name]; ok {
            out = append(out, v)
        }
    }

    return out
}

type tagList []*tag
func (tl tagList) Len() int { return len(tl) }
func (tl tagList) Swap(i, j int) { tl[i], tl[j] = tl[j], tl[i] }
func (tl tagList) Less(i, j int) bool {
    return tl[i].Normal.Count < tl[j].Normal.Count
}

func (tl tagList) String() string {
    var names []string

    for _, tag := range tl {
        names = append(names, tag.Name)
    }

    return strings.Join(names, " ")
}

// Returns a generator function which, on each call, returns the next most
// recent thread that either includes ALL of the named tags in the filtered
// parameter, OR includes ANY of the named tags in the merged parameter.
func makeResultGenerator(filtered, merged, excluded tagList) func() *thread {

    if len(filtered) < 1 && len(merged) < 1 {
        return func() *thread { return nil }
    }

    found := make(map[*thread]bool)
    nextFilteredThread := filterGenerator(filtered)
    nextMergedThread := mergeGenerator(merged)

    ft := nextFilteredThread()
    mt := nextMergedThread()

    advance := func() *thread {
        var out *thread
        if ft == nil {
            if ft != nil {
                log.Printf("ft nil, returning mt %d", ft.Updated)
            }
            out = mt
            mt = nextMergedThread()
        } else if mt == nil {
            if mt != nil {
                log.Printf("mt nil, returning ft %d", mt.Updated)
            }
            out = ft
            ft = nextFilteredThread()
        } else {
            if ft.Updated.After( mt.Updated ) {
                log.Printf("ft %d > mt %d, returning ft", ft.Updated, mt.Updated)
                out = ft
                ft = nextFilteredThread()
            } else {
                log.Printf("mt %d > ft %d, returning mt", mt.Updated, ft.Updated)
                out = mt
                mt = nextMergedThread()
            }
        }

        return out
    }

    ignorable := func(t *thread) bool {
        for _, actual := range t.Tags {
            for _, forbidden := range excluded {
                if actual == forbidden.Name {
                    return true
                }
            }
        }
        return false
    }

    return func() *thread {
        for {
            t := advance()
            if t == nil {
                return nil
            } else if ignorable(t) {
                found[t] = true
                continue
            } else if _, ok := found[t]; ok {
                continue
            }

            found[t] = true
            return t
        }
        return nil // never reached.
    }
}

func containsAllTags(t *thread, tags tagList) bool {
    for _, tag := range tags {
        if _, ok := tag.Normal.Elems[t]; !ok {
            return false
        }
    }
    return true
}


// Returns a generator function which, on each call, returns the next most
// recent thread that includes all of the named tags in the labels parameter.
func filterGenerator(filterTags tagList) func() *thread {

    if len(filterTags) == 0 {
        return func() *thread { return nil }
    }

    sort.Sort(filterTags)
    threadNode := filterTags[0].Normal.Threads.Front()

    return func() *thread {
        for {
            if threadNode == nil {
                return nil
            }

            t := threadNode.Value.(*thread)
            threadNode = threadNode.Next()
            if containsAllTags(t, filterTags) {
                return t
            }
        }
        return nil
    }
}

// Returns a generator function which, on each call, returns the next most
// recent thread from any of the named tags in the labels parameter.
func mergeGenerator(mergeTags tagList) func() *thread {

    threadLists := []*list.Element{}

    for _, tag := range mergeTags {
        threadLists = append(threadLists, tag.Normal.Threads.Front())
    }

    return func() *thread {
        if len(threadLists) == 0 {
            return nil
        }

        var newestIndex int
        for i, _ := range threadLists {

            if threadLists[i] == nil {
                continue
            }

            t := threadLists[i].Value.(*thread)
            newest := threadLists[newestIndex]

            if newest == nil || t.Updated.After(
                                    newest.Value.(*thread).Updated ) {
                newestIndex = i
            }
        }

        if threadLists[newestIndex] == nil {
            return nil
        }

        out := threadLists[newestIndex]
        threadLists[newestIndex] = threadLists[newestIndex].Next()
        return out.Value.(*thread)
    }
}

func (h *hive) assembleResultsPage(normal, sticky []*thread,
    count int, page int, search parsedQuery) {

    type pageData struct {
        Page        int
        PageRange   []int
        Query       parsedQuery
    }

    pageList := new(bytes.Buffer)
    e := templates.ExecuteTemplate(pageList, "page_nav",
            &pageData{ page, pageRange(page, count), search})
    if e != nil {
        log.Println(e)
    }

    type queryData struct {
        Normal      []*thread
        Sticky      []*thread
        Tags        []string
        Query       parsedQuery
        Pages       []byte
        ThreadForm  template.HTML
        Settings    *tolxankaConfigToml
        Insecure    bool
    }

    var data *queryData
    h.UpdateThreadForm()
    data = &queryData{  normal, sticky, search.Merge,
                        search, pageList.Bytes(), h.ThreadForm,
                        settings, settings.Debug.insecureMode}

    search.WriteOut.Header().Set("Content-Encoding", "gzip")
    search.WriteOut.Header().Set(
            "Content-Type", "application/xhtml+xml; charset=UTF-8")

    gz := gzip.NewWriter(search.WriteOut)
    defer gz.Close()

    e = templates.ExecuteTemplate(gz, search.View, data)
    if e != nil {
        log.Println(e)
    }
}

type threadList struct {
    Threads     *list.List
    Elems       map[*thread]*list.Element
    Count       uint
}

func newThreadList() *threadList {
    tl := new(threadList)
    tl.Threads = list.New().Init()
    tl.Elems = map[*thread]*list.Element{}
    return tl
}

func (tl *threadList) AddThread(t *thread) {
    _, ok := tl.Elems[t]
    if !ok {
        e := tl.Threads.PushFront(t)
        tl.Elems[t] = e
        tl.Count++
    }
}

func (tl *threadList) RemoveThread(t *thread) {
    e, ok := tl.Elems[t]
    if ok {
        tl.Threads.Remove(e)
        delete(tl.Elems, t)
        tl.Count--
    }
}

type tag struct {
    Name            string
    Normal          *threadList
    Sticky          *threadList
}

func newTag(title string) *tag {
    label := new(tag)
    label.Name = title
    label.Normal = newThreadList()
    label.Sticky = newThreadList()
    return label
}

func (label *tag) StickyThread(t *thread) {
    label.Normal.RemoveThread(t)
    label.Sticky.AddThread(t)
}

func (label *tag) UnstickyThread(t *thread) {
    label.Sticky.RemoveThread(t)
    label.Normal.AddThread(t)
}

func (label *tag) AddThread(t *thread) {
    label.Normal.AddThread(t)
}

// Find the list node for a thread in the lookup map, remove it
// from the list, reinsert the thread at the front of the list,
// and enter the new node back into the lookup map in the place
// of the old one.
func (label *tag) Bump(t *thread) {
    e, ok := label.Normal.Elems[t]
    if !ok {
        return
    }

    label.Normal.Threads.MoveToFront(e)
    label.Normal.Elems[t] = e
}

func RemoveSpecialLabels(tagLabels []string) []string {
    cleanLabels := []string{}
    for _, label := range tagLabels {
        if strings.HasPrefix(label, "!!") {
            continue
        }
        cleanLabels = append(cleanLabels, label)
    }
    return cleanLabels
}

