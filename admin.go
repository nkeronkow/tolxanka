package main

import (
	"bytes"
	"golang.org/x/crypto/openpgp"
	"encoding/json"
	"errors"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"log"
	"math/rand"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type adminChallenge struct {
	Text       string
	Expiration time.Time
}

// http handler to show admin login page.
func showAdminLogin(w http.ResponseWriter, r *http.Request) {
	ip := strings.SplitN(r.RemoteAddr, ":", 2)[0]
	challenge, _ := siteUsers.IssueAdminChallenge(ip)

	templates.ExecuteTemplate(w, "admin_login", struct {
		Challenge string
		Settings  *tolxankaConfigToml
	}{challenge, settings})
}

func postAdminResponse(w http.ResponseWriter, r *http.Request) {
	ip := strings.SplitN(r.RemoteAddr, ":", 2)[0]
	r.ParseForm()
	response := r.Form.Get("response")
	staffName := r.Form.Get("staff_name")

	challenge, isNew := siteUsers.IssueAdminChallenge(ip)
	if isNew {
		showAdminLogin(w, r)
		return
	}

	if e := checkKey(staffName, challenge, response); e != nil {
		log.Println("failed admin sig check: " + e.Error())
		msg(w, 200, "admin_login_failure")
		return
	}

	log.Println("Authentication successful")
	beginStaffSession(r, w, staffName)
}

func checkKey(staffName, challenge, response string) error {
	staffUser, ok := settings.Staff[staffName]
	if !ok {
		return errors.New("User " + staffName + " not found.")
	}

	if !staffUser.Active {
		return errors.New("User " + staffName + " has been disabled.")
	}

	snr := strings.NewReader
	keyBuf := snr(staffUser.PublicKey)

	keyRing, e := openpgp.ReadArmoredKeyRing(keyBuf)
	if e != nil {
		return e
	}

	_, e1 := openpgp.CheckArmoredDetachedSignature(
		keyRing, snr(challenge), snr(response))
	return e1
}

func postLock(w http.ResponseWriter, r *http.Request) {
	if !getStaffRole(r).LockThread {
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		return
	}

	tid := parts[2]
	val := parts[3]

	if val == "true" {
		log.Println("Locking thread " + tid)
		lockThread(threadId(tid), true)
	} else {
		log.Println("Unlocking thread " + tid)
		lockThread(threadId(tid), false)
	}

	http.Redirect(w, r, "/t/"+tid, http.StatusFound)
}

func showStickyLanding(w http.ResponseWriter, r *http.Request) {
	if !getStaffRole(r).StickyThread {
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		return
	}

	tid := threadId(parts[2])

	hiveReq(func(h *hive) {
		t, ok := h.Threads[tid]
		if !ok {
			return
		}
		e := templates.ExecuteTemplate(w, "sticky_landing", t)
		if e != nil {
			log.Panic(e)
		}
	})
}

func showAdminRights(w http.ResponseWriter, r *http.Request) {
	if role := getStaffRole(r); role.Title == "" {
		msg(w, 404, "404")
	} else {
		json.NewEncoder(w).Encode(role)
	}
}

func postSticky(w http.ResponseWriter, r *http.Request) {
	if !getStaffRole(r).StickyThread {
		return
	}

	r.ParseForm()
	tags := r.Form["tag"]
	tid := threadId(r.Form.Get("thread_no"))
	hiveReq(func(h *hive) {
		h.StickyThread(tid, tags)
	})

	passthrough(w, "thread_stickied", "/t/"+string(tid))
}

func showDeleteThread(w http.ResponseWriter, r *http.Request) {
	if !getStaffRole(r).DeleteThread {
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		return
	}

	tid := threadId(parts[2])

	e := templates.ExecuteTemplate(w, "delete_thread", tid)
	if e != nil {
		log.Println(e)
	}
}

func postDeleteThread(w http.ResponseWriter, r *http.Request) {
	if !getStaffRole(r).DeleteThread {
		return
	}

	r.ParseForm()
	tid := r.Form.Get("tid")
	hiveReq(func(h *hive) {
		h.DeleteThread(threadId(tid))
	})

	passthrough(w, "thread_deleted", "/")
}

func postModPostsLanding(w http.ResponseWriter, r *http.Request) {
	if !getStaffRole(r).DeletePost && !getStaffRole(r).BanUser {
		return
	}

	r.ParseForm()
	postIds := []postGid{}

	for _, gidString := range r.Form["selected_post"] {
		gid, e := strconv.ParseUint(gidString, 10, 64)
		if e != nil {
			return
		}

		postIds = append(postIds, postGid(gid))
	}

	hiveReq(func(h *hive) {
		posts := []*post{}

		for _, gid := range postIds {
			if p := h.GetPost(gid); p != nil {
				posts = append(posts, p)
			}
		}

		page := h.ModPosts(posts)
		w.Write(page)
	})
}

func postModPosts(w http.ResponseWriter, r *http.Request) {
	if !getStaffRole(r).DeletePost && !getStaffRole(r).BanUser {
		return
	}

	r.ParseForm()
	reason := banReason{}
	reason.Name = r.Form.Get("ban_reason")
	log.Println("ban_reason: " + reason.Name)
	if reason.Name == "" {
		msg(w, 200, "invalid_fields")
		return
	}

	reason.Description = r.Form.Get(reason.Name + "_ban_desc")
	log.Println("ban_desc: " + reason.Description)
	if reason.Description == "" {
		msg(w, 200, "invalid_fields")
		return
	}

	var e error
	length := r.Form.Get(reason.Name + "_ban_length")
	log.Println("length: " + length)
	reason.Length, e = strconv.ParseUint(length, 10, 64)
	if e != nil {
		msg(w, 200, "invalid_fields")
		return
	}

	log.Println(reason)
	affectedUsers := map[string]bool{}

	for _, gidString := range r.Form["gid"] {
		hiveReq(func(h *hive) {
			gid, e := strconv.ParseUint(gidString, 10, 64)
			if e != nil {
				return
			}

			p := h.GetPost(postGid(gid))
			if p == nil {
				return
			}

			affectedUsers[p.UserAddr] = true

			if p.Media != nil && r.Form.Get("block_Media") != "" {
				if mediaStore.Block(p.Media, reason) != nil {
					msg(w, 200, "Media_block_failed")
					return
				}
			}

			if r.Form.Get("delete_post") != "" {
				if !getStaffRole(r).DeletePost {
					return
				}
				h.HidePost(p)
			}
		})
	}

	if reason.Name != "no_ban" {
		if !getStaffRole(r).BanUser {
			return
		}

		for ip, _ := range affectedUsers {
			siteUsers.IssueBan(ip, reason)
		}
	}

	passthrough(w, "actions_complete", "/")
}

func postPostsByUser(w http.ResponseWriter, r *http.Request) {
	if !getStaffRole(r).ShowUserPosts {
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		return
	}

	gid, e := strconv.ParseUint(parts[2], 10, 64)
	if e != nil {
		msg(w, 200, "invalid_fields")
		return
	}

	hiveReq(func(h *hive) {
		p := h.GetPost(postGid(gid))
		if p == nil {
			msg(w, 200, "invalid_fields")
			return
		}

		posts := h.PostsByAddr(p.UserAddr)
		page := h.ModPosts(posts)
		w.Write(page)
	})
}

// Fill out mail template and send to admin.
func mailNotify(ip string, p *post) {
	log.Println("mailNotify")
	addresses := strings.Split(settings.Notify.Addr, ",")
	if len(addresses) < 1 {
		return
	}

	msg := new(bytes.Buffer)
	data := struct {
		Addr string
		Post *post
	}{ip, p}

	e := text.ExecuteTemplate(msg, "post_reported", data)
	if e != nil {
		log.Println(e)
	}

	auth := smtp.PlainAuth("", settings.Notify.Addr, settings.Notify.Password,
		strings.Split(settings.Notify.SMTPServer, ":")[0])
	e = smtp.SendMail(settings.Notify.SMTPServer, auth,
		settings.Notify.FromEmail, addresses, msg.Bytes())
	if e != nil {
		log.Println("ERROR: Failed to send notification email: " + e.Error())
	}
}

func beginStaffSession(r *http.Request, w http.ResponseWriter, name string) {
	s, _ := staffSessions.New(r, settings.Admin.CookieName)
	s.Values["id"] = name

	if e := staffSessions.Save(r, w, s); e != nil {
		log.Panic(e)
	}

	passthrough(w, "admin_login_success", "/")
}

// Check request for admin cookie and return user name.
func getStaffName(r *http.Request) string {
	s, e := staffSessions.Get(r, settings.Admin.CookieName)
	if s.IsNew || e != nil {
		return ""
	}

	return s.Values["id"].(string)
}

func getStaff(r *http.Request) (Staff, error) {
	staffName := getStaffName(r)
	staff, ok := settings.Staff[staffName]
	if !ok {
		return Staff{}, errors.New("Staff not found")
	}
	return staff, nil
}

// Check request for admin cookie and return role.
func getStaffRole(r *http.Request) Role {
	staff, e := getStaff(r)
	if e != nil || !staff.Active {
		return Role{}
	}

	if role, ok := settings.Roles[staff.Role]; !ok {
		return Role{}
	} else {
		return role
	}
}

func randomBytes(length int) []byte {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz" +
		"0123456789+/"

	b := make([]byte, length)

	for i, _ := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}

	return b
}

func initSessionStore() *sessions.CookieStore {
	rKey := securecookie.GenerateRandomKey
	return sessions.NewCookieStore(rKey(64), rKey(32))
}
