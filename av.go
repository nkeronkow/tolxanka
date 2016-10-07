package main

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os/exec"
	"strconv"
)

var audioThumbnail []byte

type probeResult struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	CodecName string `json:"codec_name"`
	CodecType string `json:"codec_type"`
	Height    int    `json:"height"`
	Width     int    `json:"width"`
}

type probeFormat struct {
	BitRate  int       `json:"bit_rate,string"`
	Duration string    `json:"duration"`
	Tags     probeTags `json:"tags"`
}

type probeTags struct {
	Album  string `json:"album"`
	Artist string `json:"artist"`
	Title  string `json:"title"`
}

func ffmpegThumbCmd(fileName string) *exec.Cmd {
	sec := settings.Video.ThumbnailSeekTime.Duration.Seconds()
	scale := fmt.Sprintf("scale=w=%d:h=-1", settings.Image.ThumbWidth)

	return exec.Command(
		settings.Video.FfmpegPath,
		"-i", fileName,
		"-ss", strconv.FormatFloat(sec, 'f', 0, 64),
		"-vframes", "1",
		"-f", "mjpeg",
		"-vf", scale,
		"pipe:1")
}

func ffprobeCmd(fileName string) *exec.Cmd {
	return exec.Command(
		settings.Video.FfprobePath,
		"-v", "quiet",
		"-of", "json",
		"-show_format",
		"-show_streams",
		fileName)
}

var ffmpegWork chan func()

func startFfmpegWorkers() {
	ffmpegWork = make(chan func())
	for i := 0; i < settings.Video.Workers; i++ {
		go func() {
			for {
				(<-ffmpegWork)()
			}
		}()
	}
}

func (lib *library) InsertVideo(vid io.ReadSeeker) (*media, error) {
	video, e := ioutil.ReadAll(vid)
	if e != nil {
		return nil, e
	}

	i := new(media)

	i.MediaType = "video"
	i.Full = video
	i.Size = len(i.Full)
	i.InMemory = true

	h := md5.New()
	h.Write(video)
	i.Hash = fmt.Sprintf("%x", h.Sum(nil))

	lib.DirectInsert(i)

	wait := make(chan bool)
	ffmpegWork <- func() {
		i.InfoString = probeVideo(i.FileName(), i.Size)
		i.Thumb, _ = webmThumb(i.FileName())
		wait <- true
	}

	<-wait
	persistMedia <- i

	return i, nil
}

func webmThumb(fileName string) ([]byte, error) {
	cmd := ffmpegThumbCmd(fileName)

	out, e := cmd.StdoutPipe()
	if e != nil {
		log.Println(e)
	}

	if e := cmd.Start(); e != nil {
		log.Println(e)
	}

	return ioutil.ReadAll(out)
}

func probeVideo(fileName string, size int) string {
	return videoInfo(probeFile(fileName), size)
}

func probeFile(fileName string) *probeResult {
	byteText, e := ffprobeCmd(fileName).Output()
	if e != nil {
		log.Println(e)
	}

	var info probeResult
	if e := json.Unmarshal(byteText, &info); e != nil {
		log.Println(e)
	}

	return &info
}

func videoInfo(pr *probeResult, size int) string {
	var audio, video string
	var width, height int

	for _, stream := range pr.Streams {
		if stream.CodecType == "audio" {
			audio = stream.CodecName
		} else if stream.CodecType == "video" {
			video = stream.CodecName
			width = stream.Width
			height = stream.Height
		}
	}

	codecs := video
	if audio != "" {
		codecs += "/" + audio
	}

	duration := durationString(pr.Format.Duration)

	return fmt.Sprintf("(%s %dx%d %s %s)",
		sizeString(size), width, height, duration, codecs)
}

func timeString(sec int) string {
	hours := sec / 3600
	minutes := (sec - (hours * 3600)) / 60
	seconds := sec - (hours * 3600) - (minutes * 60)
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func durationString(s string) string {
	seconds, _ := strconv.ParseFloat(s, 64)
	return timeString(int(seconds))
}

func (lib *library) InsertAudio(aud io.ReadSeeker) (*media, error) {
	audio, e := ioutil.ReadAll(aud)
	if e != nil {
		return nil, e
	}

	i := new(media)

	i.MediaType = "audio"
	i.Full = audio
	i.Size = len(i.Full)
	i.InMemory = true

	h := md5.New()
	h.Write(audio)
	i.Hash = fmt.Sprintf("%x", h.Sum(nil))

	lib.DirectInsert(i)

	wait := make(chan bool)
	ffmpegWork <- func() {
		i.Thumb = audioThumbnail

		i.InfoString, e = probeAudio(i.FileName(), i.Size)
		if e != nil {
			i = nil
		}

		wait <- true
	}

	<-wait
	persistMedia <- i

	return i, e
}

func probeAudio(fileName string, size int) (string, error) {
	info := probeFile(fileName)
	return audioInfo(info, size)
}

func audioInfo(pr *probeResult, size int) (string, error) {
	if len(pr.Streams) < 1 {
		return "", errors.New("no streams")
	}

	stream := pr.Streams[0]
	tags := pr.Format.Tags
	duration := durationString(pr.Format.Duration)
	bitRate := pr.Format.BitRate / 1000

	if !inList(settings.Audio.AcceptedCodecs, stream.CodecName) {
		return "", errors.New("invalid codec")
	}

	str := fmt.Sprintf("(%s %s - %s (%s) %s %dkbps %s)",
		sizeString(size), tags.Artist, tags.Title, tags.Album,
		duration, bitRate, stream.CodecName)

	return str, nil
}
