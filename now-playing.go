package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"

	"golang.org/x/net/websocket"
)

const (
	ChannelTrack     = "track"
	ChannelPlayState = "playState"
	ReadBufSize      = 1024
)

var (
	playState   = false
	WebhookURLs = []string{}
	mutex       = sync.RWMutex{}
)
var LastNotifyTrack *MessageTrack
var LastTrack *MessageTrack

type Message struct {
	Channel string `json:"channel"`
	Raw     []byte
}

type MessageTrack struct {
	Channel string `json:"channel"`
	Payload struct {
		Title    string `json:"title"`
		Artist   string `json:"artist"`
		Album    string `json:"album"`
		AlbumArt string `json:"albumArt"`
	} `json:"payload"`
}

type MessagePlayState struct {
	Payload bool `json:"payload"`
}

type SlackMessage struct {
	Text        string            `json:"text,omitempty"`
	Username    string            `json:"username"`
	IconEmoji   string            `json:"icon_emoji"`
	Attachments []SlackAttachment `json:"attachments,omitempty"`
}

type SlackAttachment struct {
	ImageUrl string  `json:"image_url,omitempty"`
	ThumbUrl string  `json:"thumb_url,omitempty"`
	Fallback string  `json:"fallback,omitempty"`
	Text     string  `json:"text,omitempty"`
	Fields   []Field `json:"fields,omitempty"`
}

type Field struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

func main() {
	if len(os.Args) < 2 {
		log.Print("please pass webhook url to command line arguments")
	}
	WebhookURLs = os.Args[1:]

	ws, err := websocket.Dial("ws://localhost:5672", "", "http://localhost")
	if err != nil {
		log.Print(err)
		return
	}

	buf := make([]byte, ReadBufSize)
	messageBuf := make([]byte, 0)
	for {
		n, err := ws.Read(buf)
		if err != nil {
			log.Print(err)
			break
		}
		messageBuf = append(messageBuf, buf[:n]...)
		if n == ReadBufSize {
			continue
		}

		message := new(Message)
		err = json.Unmarshal(messageBuf, message)
		if err != nil {
			continue
		}
		message.Raw = messageBuf
		go Dispatch(message)
		messageBuf = make([]byte, 0)
	}
}

func Dispatch(msg *Message) {
	switch msg.Channel {
	case ChannelTrack:
		trackPayload := new(MessageTrack)
		err := json.Unmarshal(msg.Raw, trackPayload)
		if err != nil {
			log.Print(err)
			return
		}
		HandleTrack(trackPayload)
	case ChannelPlayState:
		playStatePayload := new(MessagePlayState)
		err := json.Unmarshal(msg.Raw, playStatePayload)
		if err != nil {
			log.Print(err)
			return
		}
		HandlePlayState(playStatePayload)
	}
}

func HandleTrack(msg *MessageTrack) {
	log.Print(msg)
	mutex.Lock()
	defer mutex.Unlock()
	LastTrack = msg
	if playState == false {
		log.Print("not playing")
		return
	}
	Notify(msg)
}

func HandlePlayState(msg *MessagePlayState) {
	log.Printf("play state: %v", msg.Payload)
	mutex.Lock()
	defer mutex.Unlock()
	playState = msg.Payload

	if playState {
		if LastNotifyTrack != LastTrack {
			Notify(LastTrack)
		}
	}
}

func Notify(track *MessageTrack) {
	attachments := []SlackAttachment{
		SlackAttachment{
			ThumbUrl: track.Payload.AlbumArt,
			Fallback: fmt.Sprintf("%s - %s", track.Payload.Artist, track.Payload.Title),
			Fields: []Field{
				Field{Title: "Artist", Value: track.Payload.Artist, Short: false},
				Field{Title: "Album", Value: track.Payload.Album, Short: false},
				Field{Title: "Track", Value: track.Payload.Title, Short: false},
			},
		},
	}

	slackMessage := SlackMessage{
		Username:    "Now Playing",
		IconEmoji:   ":musical_note:",
		Attachments: attachments,
	}
	payload, err := json.Marshal(&slackMessage)
	if err != nil {
		return
	}
	v := url.Values{}
	v.Set("payload", string(payload))

	wg := sync.WaitGroup{}
	for _, url := range WebhookURLs {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			res, err := http.PostForm(u, v)
			if err != nil {
				return
			}
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				b, _ := ioutil.ReadAll(res.Body)
				log.Print(string(b))
				return
			}
		}(url)
	}
	wg.Wait()

	LastNotifyTrack = track
}
