package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gdamore/tcell"
	"github.com/gdamore/tcell/encoding"
)

type Author struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

type Tweet struct {
	Text      string `json:"text"`
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	AuthorID  string `json:"author_id"`
	Author    Author
	URL       string
}

type TwitterResponse struct {
	Data     []Tweet `json:"data"`
	Includes struct {
		Users []Author `json:"users"`
	} `json:"includes"`
	Meta struct {
		NewestID    string `json:"newest_id"`
		OldestID    string `json:"oldest_id"`
		ResultCount uint   `json:"result_count"`
		NextToken   string `json:"next_token"`
	} `json:"meta"`
}

func getTweets(query string, lastID string, startTime string) []Tweet {
	BEARER_TOKEN := os.Getenv("TWITTER_TOKEN")
	if BEARER_TOKEN == "" {
		log.Fatal("Missing TWITTER_TOKEN environment variable")
	}
	// @thought: Instead of lastID, use ...params and append them all?

	var retValue []Tweet
	var nextToken string
	fetch := func() {
		// Reset the token
		nextToken = ""
		url := fmt.Sprintf("https://api.twitter.com/2/tweets/search/recent?query=%s&tweet.fields=created_at&expansions=author_id", url.QueryEscape(query))
		if nextToken != "" {
			url = fmt.Sprintf("%s%s%s", url, "&next_token=", nextToken)
		} else {
			if lastID != "" {
				url = fmt.Sprintf("%s%s%s", url, "&since_id=", lastID)
			}
			if lastID == "" && startTime != "" {
				url = fmt.Sprintf("%s%s%s", url, "&start_time=", startTime)
			}
		}

		client := &http.Client{}
		request, _ := http.NewRequest("GET", url, nil)
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", BEARER_TOKEN))

		response, err := client.Do(request)
		if err != nil {
			log.Fatal(err)
		}

		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Fatal(err)
		}

		// JSON decode the body
		var twitterResponse TwitterResponse
		err = json.Unmarshal(body, &twitterResponse)
		if err != nil {
			log.Fatal(err)
		}

		// Create a map of the authors
		authors := make(map[string]Author)
		for _, author := range twitterResponse.Includes.Users {
			authors[author.ID] = author
		}

		// Inject the users into the corresponding tweets and append them to the response array
		for _, tweet := range twitterResponse.Data {
			if a, ok := authors[tweet.AuthorID]; ok {
				t := tweet
				t.Author = a
				// Build the url.
				t.URL = fmt.Sprintf("https://www.twitter.com/%s/status/%s", t.Author.Username, t.ID)
				retValue = append(retValue, t)
			}
		}

		// @todo: Restore this.
		// nextToken = twitterResponse.Meta.NextToken
	}

	fetch()

	for nextToken != "" {
		fmt.Println("Fetch more!")
		fetch()
	}

	return retValue
}

func openbrowser(url string) {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Fatal(err)
	}

}

func drawRow(s tcell.Screen, row int, text string, style tcell.Style) {
	x := 0
	y := row
	// Get width of screen.
	width, height := s.Size()

	// Replace all linebreaks with a string (so it takes up as little space as possible)
	// @todo: Find a better way to replace it.
	// text = strings.ReplaceAll(text, "\n", " ")

	// Fill the remaining space with spaces so it takes up everything
	// rowsNeeded := int(math.Ceil(float64(len(text) / width)))
	for _, r := range string(text) {
		s.SetContent(x, y, r, nil, style)
		x++
		if x >= width {
			y++
			x = 0
		}
		if y > row || row > height {
			break
		}
	}
}

type FeedType uint

const (
	TWITTER = iota
)

type FeedItem struct {
	Type FeedType
	Data interface{}
	URL  string
}

func main() {
	encoding.Register()

	s, err := tcell.NewScreen()
	if err != nil {
		log.Fatalf("%+v", err)
	}
	if err := s.Init(); err != nil {
		log.Fatalf("%+v", err)
	}

	defStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite)
	s.SetStyle(defStyle)

	quit := func() {
		s.Fini()
		os.Exit(0)
	}

	// width, height := s.Size()

	s.EnableMouse()

	twitterStyle := tcell.StyleDefault.Background(tcell.ColorBlue).Foreground(tcell.ColorWhite)

	var feed []FeedItem
	foundNew := false
	// feedMutex := sync.Mutex{}

	go func() {
		// Get the time an hour ago
		now := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)

		var lastID string
		for {
			tweets := getTweets("(@golang OR #golang OR #lostark OR #playlostark) -is:retweet", lastID, now)

			if len(tweets) > 0 {
				foundNew = true
				lastID = tweets[0].ID
				for _, t := range tweets {
					t.Text = strings.ReplaceAll(t.Text, "\n", " ")
					feed = append(feed, FeedItem{
						TWITTER,
						t,
						t.URL,
					})
				}
			}

			time.Sleep(2 * time.Second)
		}
	}()

	defer quit()

	go func() {
		for {
			ev := s.PollEvent()

			switch ev := ev.(type) {
			case *tcell.EventMouse:
				if ev.Buttons() == tcell.Button1 {
					_, y := ev.Position()
					if y < len(feed) {
						feedItem := feed[y]
						if feedItem.URL != "" {
							openbrowser(feedItem.URL)
						}
					}
				}
			case *tcell.EventResize:
				s.Sync()
			case *tcell.EventKey:
				if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyCtrlC {
					quit()
				}
			}
		}
	}()

	for {
		s.Clear()

		// Draw the feed if we have something new
		if foundNew {
			now := time.Now()
			for i, feedItem := range feed {
				switch feedItem.Type {
				case TWITTER:
					tweet := feedItem.Data.(Tweet)
					t, err := time.Parse(time.RFC3339, tweet.CreatedAt)
					if err != nil {
						log.Fatal(err)
					}
					since := now.Unix() - t.Unix()

					// Parse to hours, minutes and seconds.
					seconds := since % 60
					dateString := fmt.Sprintf("%vs", seconds)
					if since >= 60 {
						minutes := int((since % 3600) / 60)

						dateString = fmt.Sprintf("%vm %s", minutes, dateString)
					}

					text := fmt.Sprintf("[Twitter] %s - %s: %s\n", dateString, tweet.Author.Username, tweet.Text)
					drawRow(s, i, text, twitterStyle)
				}
			}
		}

		s.Show()

		time.Sleep(1 * time.Second)
	}
}
