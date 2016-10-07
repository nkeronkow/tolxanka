package main

import (
	"bytes"
	"container/list"
	"crypto/md5"
	"errors"
	"fmt"
	"github.com/nfnt/resize"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type media struct {
	Hash       string
	Full       []byte // full image
	Thumb      []byte // resized thumbnail
	MediaType  string
	InfoString string
	Size       int
	InMemory   bool
	Blocked    *banReason

	// References to this image by active threads. These are stored
	// in this structure with the images but they're managed by the
	// library itself to avoid racing with actual modifications
	// to the map of images. image pointers are deleted out of the
	// lib and left for GC once refs reaches zero.
	refs uint
}

func imageInfo(size, width, height int) string {
	return fmt.Sprintf("(%s %dx%d)", sizeString(size), width, height)
}

func sizeString(size int) string {
	var sizeString string

	if size < 1000 {
		sizeString = fmt.Sprintf("%d bytes", size)
	} else if size < (1000 * 1000) {
		sizeString = fmt.Sprintf("%d KB", size/1000)
	} else if size < (1000 * 1000 * 1000) {
		sizeString = fmt.Sprintf("%.2f MB", float64(size)/(1000*1000))
	}

	return sizeString
}

func (i *media) writeToDisk() {
	if len(i.Full) == 0 {
		log.Println("full media not in media struct, not writing to disk")
		return
	}

	imgFile, e := os.Create(i.FileName())
	if e != nil {
		log.Println("Failed creating media file: " + e.Error())
		return
	}

	_, e = imgFile.Write(i.Full)
	if e != nil {
		log.Println("Failed writing media file: " + e.Error())
		return
	}

	imgFile.Close()
}

func (i *media) FileName() string {
	baseName := i.Hash
	return filepath.Join(settings.Media.Path, baseName)
}

type library struct {
	byHash      map[string]*media
	CachedMedia *list.List
	CacheSize   int

	mtx sync.Mutex
}

func (lib *library) dispatch(media io.ReadSeeker, format string) (*media, error) {
	switch {
	case inList(settings.Image.AcceptedFileFormats, format):
		return lib.InsertImage(media)
	case inList(settings.Video.AcceptedFileFormats, format):
		return lib.InsertVideo(media)
	case inList(settings.Audio.AcceptedFileFormats, format):
		return lib.InsertAudio(media)
	}
	return nil, errors.New("invalid format")
}

func inList(list []string, s string) bool {
	for _, item := range list {
		if s == item {
			return true
		}
	}
	return false
}

// Insert image into lib. If MD5 collides with extant image, return the
// old one. Store both the original and a thumbnail version as byte slices.
// Also create full copy on disk.
func (lib *library) InsertImage(img io.ReadSeeker) (*media, error) {
	uploadedImage, e := ioutil.ReadAll(img)
	if e != nil {
		return nil, e
	}

	img.Seek(0, 0)
	c, _, e := image.DecodeConfig(img)
	if e != nil {
		log.Println("Image decoding error: " + e.Error())
		return nil, e
	}

	thumbImage, err := createThumb(img, c.Width, c.Height)
	if err != nil {
		log.Println("Thumbnailing error: " + err.Error())
		return nil, err
	}

	i := new(media)
	i.MediaType = "image"
	i.Full = uploadedImage
	i.Thumb = thumbImage
	i.Size = len(i.Full)
	i.InfoString = imageInfo((i.Size), c.Width, c.Height)
	i.InMemory = true

	h := md5.New()
	h.Write(uploadedImage)
	i.Hash = fmt.Sprintf("%x", h.Sum(nil))
	lib.DirectInsert(i)
	persistMedia <- i

	return i, nil
}

// Directly insert an already processed *media into a library. This was
// split off of the regular Insert function to aid reinsertion of old medias
// from dump files.
func (lib *library) DirectInsert(i *media) *media {
	lib.mtx.Lock()
	defer lib.mtx.Unlock()

	if old, ok := lib.byHash[i.Hash]; ok {
		return old
	}

	if !lib.clearSpace(i.Size) {
		log.Println("unable to clear space for media insertion.")
		return nil
	}

	lib.InternalInsert(i)
	i.writeToDisk()

	return i
}

func (lib *library) InternalInsert(i *media) *media {
	lib.byHash[i.Hash] = i
	lib.CachedMedia.PushBack(i)
	lib.CacheSize += i.Size
	return i
}

func (lib *library) Block(i *media, reason banReason) error {
	lib.mtx.Lock()
	defer lib.mtx.Unlock()

	if e := os.Remove(i.FileName()); e != nil {
		return e
	}

	i.Thumb = []byte{}
	i.Full = []byte{}
	i.InMemory = false
	i.Blocked = &reason

	stmt := "INSERT INTO media (hash, ban_reason) VALUES (?1, ?2);"
	if _, e := db.Exec(stmt); e != nil {
		return e
	}

	return nil
}

// loop through cached media list, removing media until the specified amount
// in memory is cleared.
func (lib *library) clearSpace(size int) bool {
	if settings.Media.CacheSize < size {
		log.Println("image above max cache size")
		return false
	}

	elem := lib.CachedMedia.Front()

	for {
		if (settings.Media.CacheSize - lib.CacheSize) > size {
			return true
		}

		if elem == nil {
			log.Println("elem is nil, breaking.")
			break
		}

		i := elem.Value.(*media)
		i.InMemory = false
		i.Full = []byte{}

		next := elem.Next()
		lib.CachedMedia.Remove(elem)
		lib.CacheSize -= i.Size
		log.Printf("Removing media %s, size: %d. Cache now: %d",
			i.Hash, i.Size, lib.CacheSize)

		elem = next
	}

	log.Println("reached end of cache list, could not clear enough space.")
	return false
}

func (lib *library) IncRef(i *media) {
	lib.mtx.Lock()
	i.refs++
	lib.mtx.Unlock()
}

func (lib *library) DecRef(i *media) {
	lib.mtx.Lock()
	i.refs--
	if i.refs < 0 {
		delete(lib.byHash, i.Hash)
	}
	lib.mtx.Unlock()
}

func (lib *library) WriteMedia(
	w http.ResponseWriter, hash string, full bool) {
	lib.mtx.Lock()
	defer lib.mtx.Unlock()

	i, ok := lib.byHash[hash]
	if !ok || i.Blocked != nil {
		return
	}

	if !full {
		w.Write(i.Thumb)
		return
	}

	if !i.InMemory {
		imgFile, e := os.Open(i.FileName())
		if e != nil {
			log.Println("Failed opening media file: " + e.Error())
			return
		}

		i.Full = make([]byte, i.Size)
		_, e = imgFile.Read(i.Full)
		if e != nil {
			log.Println("Failed reading media file: " + e.Error())
			return
		}

		i.InMemory = true
	}

	w.Write(i.Full)
}

func newLibrary() *library {
	lib := new(library)
	lib.byHash = map[string]*media{}
	lib.CachedMedia = list.New()
	lib.readFromDatabase()
	return lib
}

func (lib *library) readFromDatabase() {
	lib.mtx.Lock()
	defer lib.mtx.Unlock()

	query := "SELECT hash, thumb, type, info, size, ban_reason FROM media;"

	rows, e := db.Query(query)
	if e != nil {
		log.Panic(e)
	}
	defer rows.Close()

	for rows.Next() {
		i := new(media)

		var reasonName string
		e := rows.Scan(&i.Hash, &i.Thumb, &i.MediaType,
			&i.InfoString, &i.Size, &reasonName)
		if e != nil {
			log.Panic(e)
		}

		if reasonName != "" {
			reason, ok := settings.BanReasons[reasonName]
			if !ok {
				log.Panic("Media block ban reason not found: " + reasonName)
			}

			i.Blocked = &reason
		}

		lib.InternalInsert(i)
	}
}

func createThumb(file io.ReadSeeker, x, y int) ([]byte, error) {
	file.Seek(0, 0)
	img, _, err := image.Decode(file)
	if err != nil {
		return []byte{}, errors.New("Could not decode image.")
	}

	var newHeight, newWidth uint
	if x > y {
		newWidth = uint(settings.Image.ThumbWidth)
		newHeight = uint((settings.Image.ThumbWidth / x) * y)
	} else {
		newHeight = uint(settings.Image.ThumbHeight)
		newWidth = uint((settings.Image.ThumbHeight / x) * y)
	}

	img = resize.Resize(newWidth, newHeight, img, resize.NearestNeighbor)
	out := new(bytes.Buffer)

	jpeg.Encode(out, img, &jpeg.Options{Quality: 70})
	return out.Bytes(), nil
}
