package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"spoli/chrome"
	"strings"
	"time"

	"github.com/TheZoraiz/ascii-image-converter/aic_package"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
)

const REDIRECT_URL string = "http://127.0.0.1:8080/callback"

var html = `
<br/>
<a href="/player/play">Play</a><br/>
<a href="/player/pause">Pause</a><br/>
<a href="/player/next">Next track</a><br/>
<a href="/player/previous">Previous Track</a><br/>
<a href="/player/shuffle">Shuffle</a><br/>

`

// TODO: use PKCE
// users will not have to store their client secret

// TODO: use fzf and construct pseudo paths, e.g. songs/..., playlists/..., podcasts/...
// TODO: start player in browser tab, send requests via terminal to this tab.
// using chrome headless is a pain in the ass
var (
	auth = spotifyauth.New(
		spotifyauth.WithRedirectURL(REDIRECT_URL),
		// spotifyauth.WithScopes(spotifyauth.ScopeUserReadPrivate),
		// spotifyauth.WithScopes(spotifyauth.ScopeUserLibraryRead),
		spotifyauth.WithScopes(
			spotifyauth.ScopeUserReadCurrentlyPlaying,
			spotifyauth.ScopeUserReadPlaybackState,
			spotifyauth.ScopeUserModifyPlaybackState,
			spotifyauth.ScopeUserReadPrivate,
			spotifyauth.ScopeUserReadEmail,
			spotifyauth.ScopeStreaming,
		),
		// spotifyauth.WithScopes(spotifyauth.ScopePlaylistModifyPublic),
		// spotifyauth.WithScopes(spotifyauth.ScopePlaylistModifyPrivate),
	)
	ch = make(chan struct {
		c *spotify.Client
		t *oauth2.Token
	},
	)

	idChan = make(chan string, 1)
	tChan  = make(chan string, 1)
	state  = "abc123"
)

// TODO: on app startup
// load stored access token, compare expiry date
// if expired send old token to spotify to get new token
// store new token

func main() {

	var client *spotify.Client
	var playerState *spotify.PlayerState
	var tok string

	var chromeInstance *chrome.Instance

	loggingMiddleWare := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Println("Got request for:", r.URL.String())
			next.ServeHTTP(w, r)
		})
	}

	var stack Chain = []middleware{
		middleware(loggingMiddleWare),
	}

	http.Handle("/callback", stack.ThenFunc(func(w http.ResponseWriter, r *http.Request) {
		completeAuth(w, r)
		w.Header().Add("Content-Type", "")
		http.Redirect(w, r, "http://127.0.0.1:8080/static/player.html", http.StatusFound)
	}))

	http.Handle("/", stack.ThenFunc(func(w http.ResponseWriter, r *http.Request) {
	}))

	http.Handle("POST /id/{id}", stack.ThenFunc(func(w http.ResponseWriter, r *http.Request) {
		idString := r.PathValue("id")
		idChan <- idString
	}))

	http.Handle("GET /tok", stack.ThenFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok == "" {
			tok = <-tChan
		}
		log.Println("Sending tok: ", tok)
		w.Header().Add("Access-Control-Allow-Origin", "http://127.0.0.1:8080")
		w.Write([]byte(tok))
	}))

	http.Handle("POST /art", stack.ThenFunc(func(w http.ResponseWriter, r *http.Request) {
		urlParam := r.URL.Query().Get("url")
		log.Println("downloading from ", urlParam)
		img, err := toAscii(urlParam)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		log.Println("\n", img)
		w.Write([]byte(img))
	}))

	pwd, _ := os.Getwd()
	fDir := path.Join(pwd, "/static")
	fmt.Println(fDir)

	fs := http.FileServer(http.Dir(fDir))
	http.Handle("/static/", stack.Then(http.StripPrefix("/static", fs)))

	http.Handle("/next", stack.ThenFunc(func(w http.ResponseWriter, r *http.Request) {
		if chromeInstance == nil {
			log.Println("chrome instance not ready")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		playerUrl, err := url.JoinPath("http://127.0.0.1:8080", "/static/player.html")
		if err != nil {
			log.Printf("invalid player url '%s': %s", playerUrl, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		err = chromeInstance.Click("nextTrack")
		if err != nil {
			log.Printf("error clicking nextTrack of %s: %v\n", playerUrl, err)
		}
		w.WriteHeader(http.StatusOK)
	}))

	http.Handle("/toggle", stack.ThenFunc(func(w http.ResponseWriter, r *http.Request) {
		if chromeInstance == nil {
			log.Println("chrome instance not ready")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		playerUrl, err := url.JoinPath("http://127.0.0.1:8080", "/static/player.html")
		if err != nil {
			log.Printf("invalid player url '%s': %s", playerUrl, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		err = chromeInstance.Click("togglePlay")
		if err != nil {
			log.Printf("error clicking togglePlay of %s: %v\n", playerUrl, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	http.HandleFunc("/player/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		action := strings.TrimPrefix(r.URL.Path, "/player/")
		fmt.Println("Got request for:", action)
		var err error
		switch action {
		case "play":
			err = client.Play(ctx)
		case "pause":
			err = client.Pause(ctx)
		case "next":
			err = client.Next(ctx)
		case "previous":
			err = client.Previous(ctx)
		case "shuffle":
			playerState.ShuffleState = !playerState.ShuffleState
			err = client.Shuffle(ctx, playerState.ShuffleState)
		}
		if err != nil {
			log.Print(err)
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	})

	go func() {

		authUrl := auth.AuthURL(state)
		fmt.Println("Please log in to Spotify by visiting the following page in your browser:", authUrl)

		// wait for auth to complete
		c := <-ch
		client = c.c

		fmt.Printf("Found your %s\n", c.t.RefreshToken)
		rt, _ := auth.RefreshToken(context.Background(), c.t)
		fmt.Printf("Found your refresh %s\n", rt.AccessToken)

		tChan <- rt.AccessToken
		// use the client to make calls that require authorization
		user, err := client.CurrentUser(context.Background())
		if err != nil {
			log.Fatalf("error getting user: %s\n", err)
		}

		fmt.Println("You are logged in as:", user.ID)

		playerState, err = client.PlayerState(context.Background())
		if err != nil {
			log.Fatalf("error getting player state: %s\n", err)
		}

		go func() {
			<-time.After(time.Millisecond * 4000)
			for devId := range idChan {
				err = client.TransferPlayback(context.Background(), spotify.ID(devId), true)
				if err != nil {
					log.Printf("error transferring playback: %s\n", err)
				}
				log.Println("Transferred playback to ", devId)
			}
		}()

		fmt.Printf("Found your %s (%s)\n", playerState.Device.Type, playerState.Device.Name)

		// we use the browser and not a headless instance
		// chromeInstance = chrome.New(client, rt, "ws://127.0.0.1:9222")

		// pUrl, _ := url.JoinPath("http://127.0.0.1:8080", "/static/player.html")
		// err = chromeInstance.Start(pUrl)
		// if err != nil {
		// 	log.Println("error starting chrome: ", err)
		// }
		// log.Println("chrome instance started")

	}()

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("error starting server: %s\n", err)
	}
}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(r.Context(), state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatalf("error getting auth token: %s\n", err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}

	// use the token to get an authenticated client
	client := spotify.New(auth.Client(r.Context(), tok))
	// fmt.Fprintf(w, "Login Completed!")
	ch <- struct {
		c *spotify.Client
		t *oauth2.Token
	}{
		client,
		tok,
	}
}

const spotifySDKURL = "https://sdk.scdn.co/spotify-player.js"

func listTracks(client *spotify.Client) {
	var offset int
	var returned int = -1
	// can save one request by checking returned < limit
	for returned == -1 || returned > 0 {
		trackPage, err := client.CurrentUsersTracks(context.Background(), spotify.Limit(50), spotify.Offset(offset))
		if trackPage == nil {
			break
		}
		tracks := trackPage.Tracks
		if err != nil {
			log.Fatalf("could not get saved tracks: %v", err)
		}
		for _, t := range tracks {
			fmt.Println(t.Name)
		}
		returned = len(tracks)
		offset += returned
	}
	fmt.Println(offset)
}

type middleware func(http.Handler) http.Handler

type Chain []middleware

func (c Chain) Then(h http.Handler) http.Handler {
	for _, m := range slices.Backward(c) {
		h = m(h)
	}
	return h
}

func (c Chain) ThenFunc(h http.HandlerFunc) http.Handler {
	return c.Then(h)
}

func toAscii(url string) (string, error) {

	flags := aic_package.DefaultFlags()

	flags.Dimensions = []int{50, 25}
	flags.Colored = true
	flags.CustomMap = " .-=+#@"
	// flags.FontFilePath = "./RobotoMono-Regular.ttf" // If file is in current directory
	flags.SaveBackgroundColor = [4]int{50, 50, 50, 100}

	asciiArt, err := aic_package.Convert(url, flags)
	if err != nil {
		return "", fmt.Errorf("error converting to ASCII: %s", err)
	}
	return asciiArt, nil

}
