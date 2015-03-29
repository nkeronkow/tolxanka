package main
// antispam.go
//
// Creates post forms containing input elements that are duplicated, ordered
// unpredictably, and contain random, unintelligble names. In actuality, all
// names contain a segment "O*", where * is a number determining whether the
// input element is displayed or read by the server. If not displayed, CSS
// will select with the *= selector and move it off the page where users
// cannot see. If input is found in any of the dummy fields, the user will
// be banned.

import (
    "bytes"
    "errors"
    "fmt"
    "html/template"
    "log"
    "math/rand"
    "net/http"
    "strconv"
    "strings"
    "time"
)

func makeName(showField bool) string {
    createMarker := func(nums []int) string {
        idx := rand.Intn( len(nums) )
        return fmt.Sprintf("O%X", nums[idx])
    }

    num := uint64(0x100000000000000 + rand.Int63n(0xe00000000000000) )
    name := strings.ToUpper(strconv.FormatUint(num, 16))

    var marker string
    if showField {
        marker = createMarker(settings.SpamTrap.FieldDisplay)
    } else {
        marker = createMarker(settings.SpamTrap.FieldHide)
    }

    cut := rand.Intn( len(name) - 1 )
    return settings.SpamTrap.FieldPrefix +
            name[0:cut] + marker + name[cut:len(name)]
}

func makeFieldNames(endChar string) ([]string, string) {
    visible := rand.Intn(settings.SpamTrap.DuplicateFields-1)
    out := []string{}
    var realName string

    for i := 0; i < settings.SpamTrap.DuplicateFields; i++ {
        field :=  makeName(i == visible) + endChar
        out = append(out, field)

        if i == visible {
            realName = field
        }
    }

    return out, realName
}

func templateFields(tmplName string, names []string) template.HTML {
    out := new(bytes.Buffer)
    for _, name := range names {
        e := templates.ExecuteTemplate(out, tmplName, name)
        if e != nil {
            log.Fatal(e)
        }
    }
    return template.HTML(out.Bytes())
}

func makeFieldSeries(tmplName string) (template.HTML, string) {
    endChar := strings.ToUpper( string(tmplName[0]) )
    names, realName := makeFieldNames(endChar)
    markup := templateFields(tmplName, names)
    return markup, realName
}

// check post fields. If any of the spam trap fields are populated, ban the
// user and stop. Copy the form and and copy the valid field values into their
// canonical names.
func normalizePostFields(r *http.Request) error {
    banForSpamField := func() {
        ip := strings.SplitN(r.RemoteAddr, ":", 2)[0]
        siteUsers.IssueBanByName(ip, "Spam")
    }

    var fields fieldNames
    tid := threadId(r.Form.Get("thread_no"))

    hiveReq(func(h *hive) {
        if tid == "" {
            fields = h.ThreadFields[0]
        } else if t, ok := h.Threads[tid]; ok {
            fields = t.FieldNames
        }
    })

    if fields.isEmpty() {
        return errors.New("invalid_fields")
    }

    for k, _ := range r.MultipartForm.Value {
        if !strings.HasPrefix(k, settings.SpamTrap.FieldPrefix) {
            continue
        }

        v := r.Form.Get(k)
        if len(v) == 0 {
            continue
        }

        switch k[len(k)-1] {
        case 'C':
            if fields.Comment != k {
                banForSpamField()
                return errors.New("spam_trap")
            }
            r.Form.Add("comment", v)

        case 'R':
            if fields.ReplyTo != k {
                banForSpamField()
                return errors.New("spam_trap")
            }
            r.Form.Add("reply_to", v)
        case 'T':
            if fields.TagEntry != k {
                banForSpamField()
                return errors.New("spam_trap")
            }
            r.Form.Add("tag_entry", v)
        }
    }

    for k, _ := range r.MultipartForm.File {
        if !strings.HasPrefix(k, settings.SpamTrap.FieldPrefix) ||
                                                    k[len(k)-1] != 'U' {
            continue
        }

        v := r.MultipartForm.File[k]
        if len(v) == 0 {
            continue
        }

        if fields.Upload != k {
            banForSpamField()
            return errors.New("spam_trap")
        }
        r.MultipartForm.File["upload"] = v
    }

    return nil
}

type fieldNames struct {
    Comment     string
    ReplyTo     string
    Upload      string
    TagEntry    string
}

func (fn *fieldNames) isEmpty() bool {
    return fn.Comment == "" && fn.ReplyTo == "" &&
                fn.Upload == "" && fn.TagEntry == ""
}

func makePostForm(tid threadId) ([]byte, fieldNames) {
    type data struct {
        Comment template.HTML
        ReplyTo template.HTML
        Upload  template.HTML
        Thread  threadId
    }

    cBytes, cName := makeFieldSeries("comment")
    rBytes, rName := makeFieldSeries("reply_to")
    uBytes, uName := makeFieldSeries("upload")

    out := new(bytes.Buffer)
    e := templates.ExecuteTemplate(out, "antispam_post_form",
                            data{ cBytes, rBytes, uBytes, tid })

    if e != nil {
        log.Fatal(e)
    }

    return out.Bytes(), fieldNames{ cName, rName, uName, "" }
}

func (h *hive) UpdateThreadForm() {
    if time.Since(h.ThreadFormGenTime) <
        settings.SpamTrap.ThreadFormLifetime.Duration {

        return
    }

    type data struct {
        Comment     template.HTML
        Upload      template.HTML
        TagEntry    template.HTML
    }

    cBytes, cName := makeFieldSeries("comment")
    uBytes, uName := makeFieldSeries("upload")
    tBytes, tName := makeFieldSeries("tag_entry")

    out := new(bytes.Buffer)
    e := templates.ExecuteTemplate(out, "antispam_thread_form",
                            data{ cBytes, uBytes, tBytes })

    if e != nil {
        log.Fatal(e)
    }

    h.ThreadForm = template.HTML(out.Bytes())
    fields := fieldNames{ cName, "", uName, tName }
    if len(h.ThreadFields) >= 2 {
        h.ThreadFields = append([]fieldNames{ fields, h.ThreadFields[1] })
    } else {
        h.ThreadFields = []fieldNames{ fields }
    }

    h.ThreadFormGenTime = time.Now()
}

