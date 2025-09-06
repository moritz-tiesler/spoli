package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/TheZoraiz/ascii-image-converter/aic_package"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
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

	loggingMiddleWare := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Println("Got request for:", r.URL.String())
			next.ServeHTTP(w, r)
		})
	}

	var stack Chain = []middleware{
		middleware(loggingMiddleWare),
	}

	http.Handle("/callback", stack.ThenFunc(completeAuth))

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

		url := auth.AuthURL(state)
		fmt.Println("Please log in to Spotify by visiting the following page in your browser:", url)

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
			log.Fatal(err)
		}

		fmt.Println("You are logged in as:", user.ID)

		playerState, err = client.PlayerState(context.Background())
		if err != nil {
			log.Fatal(err)
		}

		go func() {
			for devId := range idChan {
				err = client.TransferPlayback(context.Background(), spotify.ID(devId), true)
				if err != nil {
					log.Fatal(err)
				}
				log.Println("Transferred playback to ", devId)
			}
		}()

		fmt.Printf("Found your %s (%s)\n", playerState.Device.Type, playerState.Device.Name)

		// done := make(chan struct{})
		// runChrome(client, c.t, done)
		// <-done
	}()

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(r.Context(), state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}

	// use the token to get an authenticated client
	client := spotify.New(auth.Client(r.Context(), tok))
	fmt.Fprintf(w, "Login Completed!")
	ch <- struct {
		c *spotify.Client
		t *oauth2.Token
	}{
		client,
		tok,
	}
}

const spotifySDKURL = "https://sdk.scdn.co/spotify-player.js"

func runChrome(spotifyClient *spotify.Client, tok *oauth2.Token, done chan struct{}) {

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(
		context.Background(),
		chromedp.Headless,
		chromedp.NoSandbox,
	)
	defer cancelAlloc()

	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()

	// Channel to receive the device ID from the browser's JavaScript
	deviceIDChan := make(chan string, 1)

	// Mutex to protect writing to the deviceIDChan from concurrent goroutines
	var mu sync.Mutex

	// Listen for events from the browser target
	chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *page.EventJavascriptDialogOpening:
			log.Printf("BROWSER DIALOG: %s - %s", ev.Type, ev.Message)
			go func() {
				if err := chromedp.Run(taskCtx,
					page.HandleJavaScriptDialog(true), // Accept the dialog
				); err != nil {
					log.Printf("Error handling dialog: %v", err)
				}
			}()
		case *runtime.EventBindingCalled:
			// This is where we handle calls from JavaScript to our Go-exposed function
			if ev.Name == "sendDeviceID" { // Check the binding name
				var args []string // Assuming the JS function sends a single string argument
				if err := json.Unmarshal([]byte(ev.Payload), &args); err != nil {
					log.Printf("Error unmarshaling binding payload: %v", err)
					return
				}
				if len(args) > 0 {
					log.Printf("Received device ID from browser via binding: %s", args[0])
					mu.Lock()
					select {
					case deviceIDChan <- args[0]: // Send to our Go channel
						// Successfully sent
					default:
						log.Println("Device ID channel is already full, ignoring subsequent calls.")
					}
					mu.Unlock()
				}
			}
		case *runtime.EventConsoleAPICalled:
			if ev.Type == runtime.APITypeError {
				log.Fatal(ev)
			}
			if ev.Type == runtime.APITypeLog {
				log.Fatal(ev)
			}

		}
	})

	var receivedDeviceID string

	spotifySDKInitJS := fmt.Sprintf(SDK_INIT_JS, tok.AccessToken)
	err := chromedp.Run(taskCtx,
		chromedp.Navigate("about:blank"),

		// Register the binding named "sendDeviceID".
		chromedp.ActionFunc(func(ctx context.Context) error {
			fmt.Println("running register")
			return runtime.AddBinding("sendDeviceID").Do(ctx)
		}),

		// Inject the Spotify Web Playback SDK script first
		chromedp.ActionFunc(func(ctx context.Context) error {
			fmt.Println("running inject playback sdk")
			_, err := page.AddScriptToEvaluateOnNewDocument(spotifySDKURL).Do(ctx)
			if err != nil {
				return fmt.Errorf("failed to add SDK script: %w", err)
			}
			return nil
		}),

		// Then inject our custom logic that initializes the player
		chromedp.Evaluate(spotifySDKInitJS, nil),

		// Wait for the device ID to be received from the browser via the channel
		chromedp.ActionFunc(func(ctx context.Context) error {
			fmt.Println("running wait for device id")
			select {
			case id := <-deviceIDChan:
				receivedDeviceID = id
				log.Printf("Successfully captured device ID: %s", receivedDeviceID)
				return nil
			case <-time.After(20 * time.Second):
				return fmt.Errorf("timed out waiting for Spotify player device ID")
			}
		}),
	)

	if err != nil {
		log.Println(spotifySDKInitJS)
		log.Fatalf("Chromedp error: %v", err)
	}

	if receivedDeviceID == "" {
		log.Fatal("Did not receive a device ID. Cannot proceed with playback.")
	}

	spId := spotify.ID(receivedDeviceID)

	log.Printf("Chromedp setup complete. Device ID: %s", receivedDeviceID)

	// Now use the Spotify Web API to control playback
	err = spotifyClient.TransferPlayback(taskCtx, spId, true)
	if err != nil {
		log.Fatalf("Failed to transfer playback: %v", err)
	}
	log.Printf("Playback transferred to headless device: %s", receivedDeviceID)

	trackURI := spotify.URI("spotify:track:2PpE1b4d320mSgGkM2QdGv") // Example: "Blinding Lights"
	err = spotifyClient.PlayOpt(taskCtx, &spotify.PlayOptions{
		DeviceID: &spId,
		URIs:     []spotify.URI{trackURI},
	})
	if err != nil {
		log.Fatalf("Failed to play track: %v", err)
	}
	log.Printf("Playing track: %s on device: %s", trackURI, receivedDeviceID)

	time.Sleep(10 * time.Second)

	err = spotifyClient.Pause(taskCtx)
	if err != nil {
		log.Printf("Failed to pause playback: %v", err)
	}
	log.Println("Playback paused.")

	time.Sleep(2 * time.Second)

	err = spotifyClient.Play(taskCtx)
	if err != nil {
		log.Printf("Failed to resume playback: %v", err)
	}
	log.Println("Playback resumed.")

	time.Sleep(5 * time.Second)

	log.Println("Finished. Closing browser...")

	done <- struct{}{}
}

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
